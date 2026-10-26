package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/httprate"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
)

// RateLimiter builds per-client-IP limiters.
//
// Counters live in this process's memory, so with N replicas a client
// effectively gets N× the budget. That's acceptable for an MVP; see
// docs/TODO.md for moving the counters to Redis.
type RateLimiter struct {
	Enabled    bool // false under tests so suites can hammer auth endpoints
	TrustProxy bool // key on X-Forwarded-For / X-Real-IP (only behind a trusted proxy)
}

// Limit allows `requests` per `window` per IP, responding 429 with the standard
// error envelope (and Retry-After / X-RateLimit-* headers) when exceeded.
func (l RateLimiter) Limit(requests int, window time.Duration, msg string) httpx.Middleware {
	if !l.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	key := httprate.KeyByIP
	if l.TrustProxy {
		key = httprate.KeyByRealIP
	}
	return httprate.Limit(requests, window,
		httprate.WithKeyFuncs(key),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, r, apperr.RateLimited(msg))
		}),
	)
}
