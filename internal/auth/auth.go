// Package auth handles accounts and identity: registration, login, JWT issuing
// and verification, and the middleware that puts the caller (a Principal) on
// the request context for every other feature to use.
package auth

import "context"

// Role is a user's platform role.
type Role string

const (
	RoleUser     Role = "USER"     // customer — shops, carts, checks out
	RoleMerchant Role = "MERCHANT" // seller — manages their own products
	RoleAdmin    Role = "ADMIN"    // platform operator — users + catalogue moderation
)

// Principal is the authenticated caller, decoded from the JWT.
type Principal struct {
	ID   string
	Role Role
}

// User is the public view of an account — it never carries the password hash.
type User struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	Role      Role    `json:"role"`
	StoreName *string `json:"storeName"`
}

type principalKey struct{}

// WithPrincipal returns a copy of ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the caller, if the request was authenticated.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// MustPrincipal returns the caller on routes behind Middleware.Require.
// A missing principal there is a wiring bug, so it panics (→ 500).
func MustPrincipal(ctx context.Context) Principal {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		panic("auth: MustPrincipal on a route without Require middleware")
	}
	return p
}
