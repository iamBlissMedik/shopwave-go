// Package config loads and validates the process configuration from
// environment variables. The server refuses to start half-configured: every
// problem is reported at once and the process exits.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-validated runtime configuration.
type Config struct {
	Env         string // development | test | production
	Port        int
	DatabaseURL string

	// CORSOrigins is the allowlist of frontends (user / merchant / admin apps)
	// that may call the API with credentials.
	CORSOrigins []string

	JWTSecret string
	JWTTTL    time.Duration

	AuthCookie string // httpOnly cookie holding the JWT
	CartCookie string // httpOnly cookie holding the anonymous guest-cart session id
	// CookieDomain is the registrable parent domain in production (e.g.
	// ".app.com") so cookies are shared across subdomains. Empty = host-only.
	CookieDomain string

	// TrustProxy makes the rate limiter key on X-Forwarded-For / X-Real-IP.
	// Enable only behind a proxy that sets those headers, or clients can spoof them.
	TrustProxy bool
}

func (c Config) IsProd() bool { return c.Env == "production" }
func (c Config) IsTest() bool { return c.Env == "test" }

// Load reads the environment, applies defaults, and validates the result.
func Load() (Config, error) {
	var errs []error

	cfg := Config{
		Env:          getenv("APP_ENV", "development"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		CORSOrigins:  splitList(getenv("CORS_ORIGINS", "http://localhost:3000,http://localhost:3001,http://localhost:3002")),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		AuthCookie:   getenv("COOKIE_NAME", "token"),
		CartCookie:   getenv("CART_COOKIE_NAME", "cart_sid"),
		CookieDomain: os.Getenv("COOKIE_DOMAIN"),
	}

	var err error
	if cfg.Port, err = strconv.Atoi(getenv("PORT", "4000")); err != nil {
		errs = append(errs, fmt.Errorf("PORT must be an integer: %w", err))
	}
	if cfg.JWTTTL, err = time.ParseDuration(getenv("JWT_TTL", "168h")); err != nil {
		errs = append(errs, fmt.Errorf("JWT_TTL must be a Go duration such as 168h: %w", err))
	}
	if cfg.TrustProxy, err = strconv.ParseBool(getenv("TRUST_PROXY", "false")); err != nil {
		errs = append(errs, fmt.Errorf("TRUST_PROXY must be true or false: %w", err))
	}

	switch cfg.Env {
	case "development", "test", "production":
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be development, test or production, got %q", cfg.Env))
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	minSecret := 10
	if cfg.IsProd() {
		minSecret = 32
	}
	if len(cfg.JWTSecret) < minSecret {
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d characters", minSecret))
	}

	return cfg, errors.Join(errs...)
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
