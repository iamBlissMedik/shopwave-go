// Package server wires the application together: it builds every store,
// service and handler from the config and connection pool, and mounts the
// routes behind the global middleware chain. This is the only place that knows
// about every feature — features never construct each other.
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/cors"

	"github.com/iamBlissMedik/shopwave-go/internal/admin"
	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/cart"
	"github.com/iamBlissMedik/shopwave-go/internal/config"
	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
	"github.com/iamBlissMedik/shopwave-go/internal/merchant"
	"github.com/iamBlissMedik/shopwave-go/internal/middleware"
	"github.com/iamBlissMedik/shopwave-go/internal/order"
	"github.com/iamBlissMedik/shopwave-go/internal/product"
)

// handlers groups every feature's HTTP handlers for route registration.
type handlers struct {
	authn    auth.Middleware
	auth     *auth.Handler
	products *product.Handler
	cart     *cart.Handler
	orders   *order.Handler
	merchant *merchant.Handler
	admin    *admin.Handler
}

// New returns the fully-wired HTTP handler for the API.
func New(cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger) http.Handler {
	cookies := httpx.Cookies{Domain: cfg.CookieDomain, Secure: cfg.IsProd()}
	tokens := auth.NewTokens(cfg.JWTSecret, cfg.JWTTTL)

	productStore := product.NewStore(pool)
	productSvc := product.NewService(productStore)
	cartSvc := cart.NewService(cart.NewStore(pool), productStore)

	h := handlers{
		authn: auth.NewMiddleware(tokens, cfg.AuthCookie),
		auth: auth.NewHandler(auth.NewService(auth.NewStore(pool), tokens), cartSvc, auth.HandlerConfig{
			Cookies: cookies, AuthCookie: cfg.AuthCookie, CartCookie: cfg.CartCookie,
		}),
		products: product.NewHandler(productSvc),
		cart:     cart.NewHandler(cartSvc, cookies, cfg.CartCookie),
		orders:   order.NewHandler(order.NewService(order.NewStore(pool))),
		merchant: merchant.NewHandler(merchant.NewStore(pool)),
		admin:    admin.NewHandler(admin.NewService(admin.NewStore(pool))),
	}

	mux := http.NewServeMux()
	limiter := middleware.RateLimiter{Enabled: !cfg.IsTest(), TrustProxy: cfg.TrustProxy}
	registerRoutes(httpx.NewRouter(mux), h, limiter, pool)

	return httpx.Chain(mux,
		middleware.Recover,
		middleware.RequestLog(logger),
		middleware.SecureHeaders(cfg.IsProd()),
		cors.New(cors.Options{
			// Only the user/merchant/admin frontends may call the API with
			// credentials. Other origins simply get no CORS headers.
			AllowedOrigins:   cfg.CORSOrigins,
			AllowCredentials: true,
			AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete},
			AllowedHeaders:   []string{"Content-Type", "Authorization", "Idempotency-Key", "X-Request-ID"},
			MaxAge:           int((10 * time.Minute).Seconds()),
		}).Handler,
		limiter.Limit(300, 15*time.Minute, "Too many requests, please try again later."),
	)
}
