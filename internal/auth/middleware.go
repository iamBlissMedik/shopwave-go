package auth

import (
	"net/http"
	"slices"
	"strings"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
)

// Middleware authenticates requests from the httpOnly cookie (preferred — out
// of reach of JS/XSS) or an `Authorization: Bearer <token>` header.
type Middleware struct {
	tokens     Tokens
	cookieName string
}

func NewMiddleware(tokens Tokens, cookieName string) Middleware {
	return Middleware{tokens: tokens, cookieName: cookieName}
}

// Require rejects unauthenticated requests with 401.
func (m Middleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := m.token(r)
		if token == "" {
			httpx.WriteError(w, r, apperr.Unauthorized("Authentication required"))
			return
		}
		p, err := m.tokens.Parse(token)
		if err != nil {
			httpx.WriteError(w, r, apperr.Unauthorized("Invalid or expired token"))
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

// Optional attaches the caller when a valid token is present but never
// rejects — an invalid/expired token just means "guest". Used by the cart so
// guests and customers share the same endpoints.
func (m Middleware) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := m.token(r); token != "" {
			if p, err := m.tokens.Parse(token); err == nil {
				r = r.WithContext(WithPrincipal(r.Context(), p))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (m Middleware) token(r *http.Request) string {
	if c, err := r.Cookie(m.cookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(bearer)
	}
	return ""
}

// RequireRole restricts a route to the given roles. Place it after Require.
func RequireRole(roles ...Role) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok {
				httpx.WriteError(w, r, apperr.Unauthorized("Authentication required"))
				return
			}
			if !slices.Contains(roles, p.Role) {
				httpx.WriteError(w, r, apperr.Forbidden("Insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
