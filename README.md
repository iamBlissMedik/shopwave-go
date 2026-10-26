# shopwave-go

The Shopwave multi-vendor marketplace API, written in Go. It is a port of the
Express API in `shopwave/apps/api` and keeps the **same HTTP contract**: paths,
JSON shapes, cookies and error envelope all match. The user, merchant and admin
frontends can point at it without changes.

- **Stack:** Go 1.25 · `net/http` (stdlib routing) · PostgreSQL via
  [pgx](https://github.com/jackc/pgx) · [goose](https://github.com/pressly/goose)
  migrations · `log/slog`
- **Roles:** `USER` (shops, checks out) · `MERCHANT` (sells their own products) ·
  `ADMIN` (manages users, moderates the catalogue)
- **Status:** MVP. See [docs/TODO.md](docs/TODO.md) for what's missing before production.

## Quick start

Requirements: Go 1.25+ and PostgreSQL 13+ (or Docker for `make db-up`).

```bash
cp .env.example .env          # then set JWT_SECRET (and DATABASE_URL if needed)
make db-up                    # optional: Postgres in Docker (dev + test databases)
make migrate-up               # create the schema
make seed                     # demo data (wipes the database!)
make run                      # → http://localhost:4000
```

Without Docker, create the databases yourself:
`createdb shopwave_go && createdb shopwave_go_test`.

Seeded accounts:

| Role     | Email               | Password     |
| -------- | ------------------- | ------------ |
| Admin    | `admin@example.com` | `Admin123!`  |
| Customer | `user@example.com`  | `User1234!`  |
| Merchant | `sound@example.com` | `Merchant1!` |
| Merchant | `gear@example.com`  | `Merchant1!` |

## Commands

| Command               | What it does                                                         |
| --------------------- | -------------------------------------------------------------------- |
| `make run`            | Run the API (reads `.env`)                                           |
| `make test`           | Unit + integration tests with `-race` (needs the test DB)            |
| `make test-unit`      | Unit tests only; integration tests skip without `TEST_DATABASE_URL`  |
| `make lint`           | `gofmt` check + `go vet`                                             |
| `make build`          | Static binaries in `./bin` (`api`, `migrate`)                        |
| `make migrate-up`     | Apply migrations (also `migrate-down`, `migrate-status`)             |
| `make seed`           | Wipe and seed demo data; refuses to run when `APP_ENV=production`    |

`make help` lists everything.

## Project layout

```
cmd/
  api/          HTTP server entrypoint: config, DB pool, graceful shutdown
  migrate/      Migration CLI (migrations are embedded in the binary)
  seed/         Demo data
internal/
  server/       Wiring + the route table (routes.go = whole API at a glance)
  auth/         Accounts, JWT, auth middleware, roles
  product/      Catalogue + product management (shared by merchant & admin)
  cart/         Guest + customer carts, guest→account merge
  order/        Checkout + order history
  merchant/     Merchant sales feed + dashboard stats
  admin/        User management + platform stats
  httpx/        HTTP toolkit: error-returning handlers, JSON + validation, query parsing, router
  database/     Pool, migrations, query helpers (Filter, Paginate)
  apperr/       Transport-agnostic error kinds (NotFound, Forbidden, …)
  middleware/   Recover, request log, security headers, rate limiting
  config/       Env loading + validation (fail fast)
  pagination/   Shared page params + { data, meta } response
  timeseries/   Gap-free daily chart series
docs/           Architecture notes and the TODO backlog
```

Each feature package has the same three files: **`handler.go`** (HTTP in/out),
**`service.go`** (business rules), **`store.go`** (SQL). The design, the
conventions, and a walkthrough for adding a feature are in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Configuration

All configuration comes from environment variables, with `.env` loaded in
development. Invalid config stops startup with every problem listed.

| Variable           | Default                       | Notes                                                     |
| ------------------ | ----------------------------- | --------------------------------------------------------- |
| `APP_ENV`          | `development`                 | `development` \| `test` \| `production`                   |
| `PORT`             | `4000`                        |                                                           |
| `DATABASE_URL`     | — (required)                  | Pool tunable via URL, e.g. `&pool_max_conns=20`           |
| `JWT_SECRET`       | — (required)                  | ≥10 chars; ≥32 in production                              |
| `JWT_TTL`          | `168h`                        | Go duration; also the auth cookie's max age               |
| `CORS_ORIGINS`     | `localhost:3000,3001,3002`    | Comma-separated frontend allowlist                        |
| `COOKIE_NAME`      | `token`                       | Auth cookie (httpOnly)                                    |
| `CART_COOKIE_NAME` | `cart_sid`                    | Guest-cart session cookie (httpOnly)                      |
| `COOKIE_DOMAIN`    | (host-only)                   | Production: parent domain, e.g. `.app.com`                |
| `TRUST_PROXY`      | `false`                       | Rate-limit on `X-Forwarded-For`; only behind a real proxy |

## API

All endpoints are under `/api` and return JSON. Money is always **integer
cents**. Errors use one envelope:

```json
{ "error": { "message": "Validation failed", "details": { "email": ["must be a valid email"] } } }
```

Lists take `?page` and `?limit` and return `{ "data": [...], "meta": { page, limit, total, totalPages, hasNextPage, hasPrevPage } }`.

| Method | Path                           | Access         | Notes                                                       |
| ------ | ------------------------------ | -------------- | ----------------------------------------------------------- |
| POST   | `/auth/register`               | public         | `role`: `USER` \| `MERCHANT` (merchants need `storeName`)   |
| POST   | `/auth/login`                  | public         | Sets the auth cookie; merges any guest cart                 |
| POST   | `/auth/logout`                 | public         | Clears auth + guest-cart cookies                            |
| GET    | `/auth/me`                     | logged in      |                                                             |
| GET    | `/products`                    | public         | `search, category, minPrice, maxPrice, sort` (`newest`, `price_asc`, `price_desc`, `name_asc`) |
| GET    | `/products/categories`         | public         |                                                             |
| GET    | `/products/{id}`               | public         | Active products only                                        |
| GET    | `/cart`                        | guest/customer | Guests are identified by the `cart_sid` cookie              |
| POST   | `/cart/items`                  | guest/customer | `{ productId, quantity? }`                                  |
| PATCH  | `/cart/items/{productId}`      | guest/customer | `{ quantity }`                                              |
| DELETE | `/cart/items/{productId}`      | guest/customer |                                                             |
| DELETE | `/cart`                        | guest/customer | Empty the cart                                              |
| POST   | `/orders/checkout`             | logged in      | Optional `Idempotency-Key` header                           |
| GET    | `/orders`, `/orders/{id}`      | logged in      | Own orders only                                             |
| GET    | `/merchant/stats`              | MERCHANT       | Totals, 14-day revenue, top 5 products                      |
| GET    | `/merchant/sales`              | MERCHANT       | Own order lines                                             |
| GET    | `/merchant/products`           | MERCHANT       | Own products, active and inactive; `search`                 |
| POST   | `/merchant/products`           | MERCHANT       |                                                             |
| GET    | `/merchant/products/{id}`      | MERCHANT       | Owner only                                                  |
| PATCH  | `/merchant/products/{id}`      | MERCHANT       | Partial update, owner only                                  |
| DELETE | `/merchant/products/{id}`      | MERCHANT       | Soft delete (hides the product)                             |
| GET    | `/admin/stats`                 | ADMIN          |                                                             |
| GET    | `/admin/users`                 | ADMIN          | `search, role`                                              |
| PATCH  | `/admin/users/{id}/role`       | ADMIN          | Applies at the user's next login                            |
| DELETE | `/admin/users/{id}`            | ADMIN          | 409 if a merchant's products have been sold                 |
| GET    | `/admin/products`              | ADMIN          | `search, status` (`active` \| `inactive`)                   |
| PATCH  | `/admin/products/{id}`         | ADMIN          | Moderation; e.g. `{ "isActive": false }`                    |

Probes: `GET /health` (liveness) and `GET /ready` (checks the database).

### Differences from the Express API

These are deliberate. None of them change the contract the frontends use.

- **IDs are UUIDs** (Postgres `gen_random_uuid()`), not cuids. A malformed id
  in the path returns **400**; Express returned 404.
- **Env names:** `APP_ENV` replaces `NODE_ENV`, `JWT_TTL` (Go duration)
  replaces `JWT_EXPIRES_IN`, and `TRUST_PROXY` is new. The cookie lifetime now
  follows the JWT TTL; Express hard-coded 7 days.
- **Idempotency keys are scoped per user**, so one customer's key can't collide
  with another's.
- **Checkout refuses deactivated products**, even ones already in a cart.
- **Every product response includes `merchant`**, not only some endpoints.
- **Reading a cart never creates one.** Express created a cart row on every guest `GET`.
- **No Swagger UI yet** (`/docs`). See [TODO](docs/TODO.md).

## Testing

```bash
make test
```

Integration tests (`internal/server/*_test.go`) run the real router against a
real Postgres. They migrate and then **truncate** the database in
`TEST_DATABASE_URL`, so point it at a disposable database. They cover auth,
access control, the catalogue, guest-cart merging, checkout (stock, idempotency,
price snapshots), a concurrent "last unit" race, and both dashboards. Pure
helpers (validation, pagination, slugs, time series) have plain unit tests.

## Deploying

```bash
docker build -t shopwave-go .
docker run --rm -e DATABASE_URL=... shopwave-go migrate up   # release step
docker run -p 4000:4000 -e DATABASE_URL=... -e JWT_SECRET=... shopwave-go
```

The server is stateless; sessions are JWTs and carts live in Postgres. You can
run as many replicas as you like behind a load balancer. The one caveat is
that rate-limit counters are per instance (see [TODO](docs/TODO.md)).
