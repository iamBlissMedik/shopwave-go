# Architecture

This is how the service is put together, the conventions to keep, and how to
add a feature. The goal is a codebase that stays boring as it grows: every
feature looks the same, and shared concerns are written once.

## Request lifecycle

```
request
  → Recover → RequestLog → SecureHeaders → CORS → global rate limit   (server.New)
  → ServeMux route match                                              (server/routes.go)
  → group middleware: auth.Require / auth.Optional, RequireRole, route limits
  → handler   parse + validate input (httpx), call the service, write JSON
  → service   business rules; returns domain values or *apperr.Error
  → store     SQL via pgx; runs on the pool or inside a transaction
```

Every handler has the signature `func(w, r) error`. Any error it returns is
rendered by `httpx.WriteError`:

| Error                          | Status | Body message                       |
| ------------------------------ | ------ | ---------------------------------- |
| `*apperr.Error`                | by kind (400/401/403/404/409/429) | its message, plus field `details` |
| `*http.MaxBytesError`          | 413    | "Request body too large"           |
| `pgx.ErrNoRows` (untranslated) | 404    | "Record not found"                 |
| unique violation (untranslated)| 409    | "A record with these details…"     |
| anything else                  | 500    | "Internal server error" (logged)   |

The last three are safety nets. Services should translate the cases they
expect themselves, because their messages are better.

## Layers and their rules

**handler.go** knows HTTP and nothing about SQL. It decodes and validates input
with `httpx.Decode`, `httpx.NewQuery` and `httpx.PathID`, reads the caller with
`auth.MustPrincipal`, calls one service method and writes the response.

**service.go** holds the business rules: ownership checks, stock checks,
idempotency, transactions. It returns `apperr` errors and never imports
`net/http`, so the same code could serve a worker or CLI. Request input types
live here too (`CreateInput`, …); they carry `validate` tags plus optional
`Normalize()` (trim/lowercase/defaults) and `Check()` (rules that tags can't
express).

**store.go** holds the SQL and nothing else. Each store wraps a `database.DB`,
which is satisfied by both the pool and a `pgx.Tx`. Stores that need
transactions expose `InTx(ctx, func(*Store) error)`, and the service decides
the transaction boundary.

Skipping a layer is fine when it has nothing to add. `merchant` has no service,
because its dashboard is plain reads. Add one when rules appear; don't create
empty pass-through layers in advance.

## Dependencies between features

```
server ──► every feature (the only place that wires things together)

auth      ◄── product, cart, order, merchant, admin   (Principal, roles, middleware)
product   ◄── cart, order                             (Product type, Columns/ScanDest, Store)
cart      ──► auth via auth.GuestCartMerger interface  (no import cycle)
```

Rules:
- Features never construct each other. `server.New` builds everything and
  injects it.
- When feature A needs B and B already depends on A, declare a small interface
  in A (see `auth.GuestCartMerger`) and let `server` pass B in.
- The product row shape is defined once (`product.Columns`, `product.From`,
  `product.ScanDest`). Cart and order embed it in their own queries.

## Shared building blocks (write it once)

| Need                         | Use                                                           |
| ---------------------------- | ------------------------------------------------------------- |
| Parse + validate a JSON body | `httpx.Decode(w, r, &in)`                                     |
| Query params with bounds     | `q := httpx.NewQuery(r); q.Page(def, max); q.OneOf(...); q.Err()` |
| A UUID path param            | `httpx.PathID(r, "id")`                                       |
| Paginated list endpoint      | `database.Paginate(ctx, db, ListQuery{...}, page, scanFn)`    |
| Optional WHERE filters       | `database.Filter` (`f.Where("col = " + f.Arg(v))`)            |
| Client-facing error          | `apperr.NotFound("…")`, `apperr.Forbidden("…")`, …            |
| Daily chart series           | `database.DailyTotals` + `timeseries.Daily`                   |
| Set/clear cookies            | `httpx.Cookies`                                               |

## Data and correctness

- **Money** is `bigint` cents end to end. Never use floats.
- **`updated_at`** is maintained by a trigger, so queries can't forget it.
- **Soft delete:** products are hidden (`is_active = false`), never removed.
  `order_items` references products with `ON DELETE RESTRICT` to protect
  order history.
- **Checkout** (`order.Service.Checkout`) runs as a single transaction:
  - Stock is decremented with a guarded `UPDATE … WHERE stock >= qty`, so two
    buyers can never both take the last unit (`TestCheckoutNeverOversells`).
  - Cart lines are processed in product-id order, so concurrent checkouts lock
    rows in the same order and can't deadlock.
  - Item prices and the order total are snapshots taken at checkout.
  - An optional `Idempotency-Key` makes retries safe. The unique
    `(user_id, idempotency_key)` constraint settles a concurrent double submit.
- **Cart adds** increment in SQL (`ON CONFLICT … quantity + EXCLUDED.quantity`),
  so concurrent adds never lose an update.
- **Guest → account merge** is one `INSERT … SELECT … ON CONFLICT` inside a
  transaction. Quantities are summed and clamped to stock.

## Security model

- JWT (HS256, algorithm pinned) in an httpOnly, SameSite=Lax cookie that is
  `Secure` in production. A `Bearer` header also works for non-browser clients.
- The role lives in the token, so role changes apply at the next login (a
  stateless trade-off; see TODO).
- Authorization has two layers: `RequireRole` on route groups, plus ownership
  checks in services (merchants touch only their own products; admins bypass).
- Login returns the same error, and takes about the same time, for an unknown
  email and for a wrong password.
- The CORS allowlist only reflects configured origins. Bodies are capped at
  10 KiB. Security headers are set on every response. Per-IP rate limits are
  global, stricter on `/auth`, and strictest on `/admin`.
- Logs never contain bodies or headers, so passwords and tokens can't leak.

## Adding a feature (walkthrough)

Say you're adding product **reviews**:

1. **Migration:** `internal/database/migrations/00002_reviews.sql` with
   `-- +goose Up` / `-- +goose Down` sections. Add the `updated_at` trigger if
   the table has that column. Run `make migrate-up`.
2. **Package `internal/review`:**
   - `store.go`: the `Review` type, a `const columns`, a `scan(pgx.Row)` func,
     and the queries. Use `database.Paginate` for lists.
   - `service.go`: input types with `validate` tags and the rules (e.g. "only
     customers who bought the product may review it"), returning `apperr`
     errors.
   - `handler.go`: one method per endpoint in the `func(w, r) error` shape.
3. **Wire it** in `server.New` (construct store → service → handler) and add
   the routes in `server/routes.go` under the right group and middleware.
4. **Test it:** add a scenario to `internal/server/api_test.go` using the
   existing helpers (`register`, `createProduct`, `client.expect`, …).

## Scaling notes

- The app is stateless, so scale it horizontally. Postgres is the only shared
  state; the per-instance rate limiter is the exception (see TODO).
- Every list is paginated and every dashboard query is a bounded aggregate.
  Nothing loads a whole table into memory, and order items are loaded in one
  query per page (no N+1).
- Indexes back every filter, sort and foreign key the queries use. See
  `00001_init.sql`.
- Known future hotspots, and what to do about them, are in
  [TODO.md](TODO.md#scale): substring search, deep offset pagination, and
  dashboard aggregates.
