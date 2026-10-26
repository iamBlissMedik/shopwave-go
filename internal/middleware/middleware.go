// Package middleware holds the cross-cutting HTTP middleware applied to every
// request: panic recovery, request IDs + access logs, security headers, and
// rate limiting.
package middleware

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
)

// Recover turns a panic in any handler into a 500 JSON response instead of a
// dropped connection, and logs the stack trace.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler { // deliberate abort; let net/http handle it
				panic(rec)
			}
			slog.ErrorContext(r.Context(), "panic", "panic", rec, "stack", string(debug.Stack()))
			httpx.WriteError(w, r, fmt.Errorf("panic: %v", rec))
		}()
		next.ServeHTTP(w, r)
	})
}

// RequestLog assigns each request an ID (reusing a sane incoming X-Request-ID
// so traces can span services), echoes it back, and writes one structured
// access-log line per request. Bodies and headers are never logged, so
// passwords and tokens can't leak into logs.
func RequestLog(logger *slog.Logger) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			id := r.Header.Get("X-Request-ID")
			if id == "" || len(id) > 64 {
				id = rand.Text()
			}
			w.Header().Set("X-Request-ID", id)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			logger.LogAttrs(r.Context(), level, "request",
				slog.String("request_id", id),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote", r.RemoteAddr),
			)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// SecureHeaders sets conservative security headers for a JSON API
// (the useful subset of what helmet does in the Express version).
func SecureHeaders(hsts bool) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
