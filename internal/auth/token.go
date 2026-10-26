package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Tokens issues and verifies stateless HS256 session tokens.
//
// The role is embedded in the token, so a role change takes effect on the
// user's next login (see docs/TODO.md: refresh tokens / revocation).
type Tokens struct {
	secret []byte
	ttl    time.Duration
}

func NewTokens(secret string, ttl time.Duration) Tokens {
	return Tokens{secret: []byte(secret), ttl: ttl}
}

// TTL is how long an issued token (and its cookie) stays valid.
func (t Tokens) TTL() time.Duration { return t.ttl }

type claims struct {
	Role Role `json:"role"`
	jwt.RegisteredClaims
}

func (t Tokens) Issue(p Principal) (string, error) {
	now := time.Now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: p.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   p.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(t.ttl)),
		},
	}).SignedString(t.secret)
}

// Parse verifies the signature and expiry. The algorithm is pinned to HS256,
// which shuts the door on algorithm-confusion forgeries (e.g. `alg: none`).
func (t Tokens) Parse(token string) (Principal, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c,
		func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return Principal{}, err
	}
	if c.Subject == "" || c.Role == "" {
		return Principal{}, errors.New("token missing subject or role")
	}
	return Principal{ID: c.Subject, Role: c.Role}, nil
}
