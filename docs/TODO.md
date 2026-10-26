# TODO

What the MVP deliberately leaves out, in rough priority order. Each item says
why it matters and where to start.

## Before production

- [ ] **Real payments.** Checkout marks orders `PAID` synchronously
      (`order.Service.Checkout`). Integrate a provider such as Stripe: create
      the order as `PENDING`, take payment, and move it to `PAID` from a
      verified webhook. If payment fails or expires, release the stock. The
      `status` column already supports this.
- [ ] **Order cancellation / refunds.** There's no path to `CANCELLED` yet.
      When added, restore stock in the same transaction.
- [ ] **Session revocation.** JWTs can't be revoked, so a deleted or demoted
      user keeps access until the token expires (`JWT_TTL`, default 7 days).
      Options: a `token_version` column on `users` that tokens carry and
      `auth.Middleware` checks (cheap), or short-lived access tokens plus
      refresh tokens.
- [ ] **Admin bootstrap.** Today the only way to create the first admin is
      `cmd/seed`, which wipes the database. Add a small `cmd/createadmin` (or
      `promote <email>`) for production.
- [ ] **CI.** Add a GitHub Actions workflow with a Postgres service that runs
      `make lint` and `make test`.
- [ ] **OpenAPI docs.** The Express API served Swagger at `/docs`. Write
      `docs/openapi.yaml` (or generate it) and serve it. Add a test that every
      route in `routes.go` is documented, like the Express coverage test.
- [ ] **Distributed rate limiting.** Limiter counters live in each process's
      memory, so N replicas allow N× the budget. Switch to Redis
      (`github.com/go-chi/httprate-redis` drops into `middleware.RateLimiter`)
      before running more than one replica.
- [ ] **Password reset and email verification.**

## Scale

- [ ] **Search.** `ILIKE '%term%'` can't use a B-tree index and becomes a
      sequential scan as the catalogue grows. Add a `pg_trgm` GIN index on
      `products(name, description)`, or Postgres full-text search with a
      `tsvector` column. Move to a search engine only if relevance ranking or
      facets are needed.
- [ ] **Keyset pagination.** `OFFSET` gets slower on deep pages. For infinite
      scroll or large admin lists, add cursor pagination (`WHERE (created_at,
      id) < ($1, $2)`) next to `database.Paginate`.
- [ ] **Dashboard stats.** These are computed per request with aggregates.
      Cache them briefly (30–60 s) or maintain a daily rollup table once order
      volume grows.
- [ ] **Guest-cart cleanup.** Guest carts are never deleted. Add a periodic job
      (cron or a goroutine with a ticker) that deletes carts with a
      `session_id` and `updated_at` older than 30 days. `Store.Ensure` already
      bumps `updated_at` on use.
- [ ] **Read replicas.** If the catalogue becomes read-heavy, route the
      catalogue `List`/`Get` calls to a replica pool.
- [ ] **Response compression.** Usually done at the proxy/CDN. If not, add
      gzip middleware for large list responses.

## Observability

- [ ] **Metrics:** request rate, latency and status per route, plus DB pool
      stats (`pool.Stat()`), via Prometheus or OpenTelemetry.
- [ ] **Tracing:** OpenTelemetry spans for HTTP and pgx (pgx has a tracer
      hook).
- [ ] **Error-log correlation:** put the request ID on the context (in
      `middleware.RequestLog`) and include it in `httpx.WriteError` logs.

## Product features

- [ ] Merchant order fulfilment (shipped/delivered statuses), customer
      notifications.
- [ ] Image uploads to object storage (S3/R2) with signed URLs. Today
      `imageUrl` is a URL the merchant provides.
- [ ] Categories as a table (with slugs) instead of free text.
- [ ] Product reviews; multiple images per product.
- [ ] Merchant payouts / commission accounting.
- [ ] Stock movement audit log.

## Code health

- [ ] Add `golangci-lint` with a small config (errcheck, staticcheck, revive).
- [ ] Unit-test service rules with an in-memory fake once they grow beyond
      what the integration suite covers comfortably.
