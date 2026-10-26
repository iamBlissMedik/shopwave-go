package server

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
	"github.com/iamBlissMedik/shopwave-go/internal/middleware"
)

// registerRoutes is the single table of every endpoint and the middleware
// guarding it. Read this file to see the whole API surface at a glance.
func registerRoutes(r *httpx.Router, h handlers, limiter middleware.RateLimiter, pool *pgxpool.Pool) {
	started := time.Now()

	// --- Probes (for load balancers / orchestrators) ---
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) error { // liveness
		return httpx.JSON(w, http.StatusOK, map[string]any{
			"status": "ok", "uptime": time.Since(started).Seconds(),
		})
	})
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) error { // readiness: DB reachable
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			return httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		}
		return httpx.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	api := r.Group("/api")

	// --- Auth --- (tighter limit on credential endpoints to slow brute force)
	authLimit := limiter.Limit(20, 15*time.Minute, "Too many auth attempts, please try again later.")
	a := api.Group("/auth")
	a.Post("/register", h.auth.Register, authLimit)
	a.Post("/login", h.auth.Login, authLimit)
	a.Post("/logout", h.auth.Logout)
	a.Get("/me", h.auth.Me, h.authn.Require)

	// --- Public catalogue ---
	p := api.Group("/products")
	p.Get("", h.products.List)
	p.Get("/categories", h.products.Categories)
	p.Get("/{id}", h.products.Get)

	// --- Cart: guests (session cookie) and logged-in customers ---
	c := api.Group("/cart", h.authn.Optional)
	c.Get("", h.cart.Get)
	c.Delete("", h.cart.Clear)
	c.Post("/items", h.cart.Add)
	c.Patch("/items/{productId}", h.cart.Update)
	c.Delete("/items/{productId}", h.cart.Remove)

	// --- Orders: any logged-in user ---
	o := api.Group("/orders", h.authn.Require)
	o.Post("/checkout", h.orders.Checkout)
	o.Get("", h.orders.List)
	o.Get("/{id}", h.orders.Get)

	// --- Merchant dashboard: MERCHANT only ---
	m := api.Group("/merchant", h.authn.Require, auth.RequireRole(auth.RoleMerchant))
	m.Get("/stats", h.merchant.Stats)
	m.Get("/sales", h.merchant.Sales)
	m.Get("/products", h.products.ListOwn)
	m.Post("/products", h.products.Create)
	m.Get("/products/{id}", h.products.GetOwned)
	m.Patch("/products/{id}", h.products.Update)
	m.Delete("/products/{id}", h.products.Delete)

	// --- Admin console: ADMIN only, strictest rate limit ---
	ad := api.Group("/admin",
		limiter.Limit(100, 15*time.Minute, "Too many admin requests, please try again later."),
		h.authn.Require, auth.RequireRole(auth.RoleAdmin))
	ad.Get("/stats", h.admin.Stats)
	ad.Get("/users", h.admin.ListUsers)
	ad.Patch("/users/{id}/role", h.admin.UpdateRole)
	ad.Delete("/users/{id}", h.admin.DeleteUser)
	ad.Get("/products", h.products.ListAll)
	ad.Patch("/products/{id}", h.products.Update) // moderation; admins bypass ownership

	// --- Fallback: JSON 404 for anything unmatched ---
	r.Handle("", "/", func(_ http.ResponseWriter, r *http.Request) error {
		return apperr.NotFound("Route " + r.Method + " " + r.URL.Path + " not found")
	})
}
