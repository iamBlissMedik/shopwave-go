package auth

import (
	"context"
	"net/http"

	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
)

// GuestCartMerger folds an anonymous guest cart into a customer's cart. It is
// implemented by the cart service; declaring it here keeps auth independent
// of the cart package.
type GuestCartMerger interface {
	MergeGuestCart(ctx context.Context, userID, sessionID string) error
}

// HandlerConfig names the cookies the auth endpoints read and write.
type HandlerConfig struct {
	Cookies    httpx.Cookies
	AuthCookie string
	CartCookie string
}

type Handler struct {
	svc   *Service
	carts GuestCartMerger
	cfg   HandlerConfig
}

func NewHandler(svc *Service, carts GuestCartMerger, cfg HandlerConfig) *Handler {
	return &Handler{svc: svc, carts: carts, cfg: cfg}
}

// Register: POST /api/auth/register
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) error {
	var in RegisterInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	user, token, err := h.svc.Register(r.Context(), in)
	if err != nil {
		return err
	}
	if err := h.startSession(w, r, user, token); err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, map[string]any{"user": user})
}

// Login: POST /api/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) error {
	var in LoginInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	user, token, err := h.svc.Login(r.Context(), in)
	if err != nil {
		return err
	}
	if err := h.startSession(w, r, user, token); err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"user": user})
}

// Logout: POST /api/auth/logout — clears the session and any guest cart cookie.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) error {
	h.cfg.Cookies.Clear(w, h.cfg.AuthCookie)
	h.cfg.Cookies.Clear(w, h.cfg.CartCookie)
	return httpx.JSON(w, http.StatusOK, map[string]string{"message": "Logged out"})
}

// Me: GET /api/auth/me
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) error {
	user, err := h.svc.Me(r.Context(), MustPrincipal(r.Context()).ID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"user": user})
}

// startSession sets the auth cookie and, for customers who shopped as a guest
// first, merges that guest cart into their account cart.
func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, user User, token string) error {
	h.cfg.Cookies.Set(w, h.cfg.AuthCookie, token, h.svc.tokens.TTL())

	c, err := r.Cookie(h.cfg.CartCookie)
	if err != nil || c.Value == "" || user.Role != RoleUser {
		return nil
	}
	if err := h.carts.MergeGuestCart(r.Context(), user.ID, c.Value); err != nil {
		return err
	}
	h.cfg.Cookies.Clear(w, h.cfg.CartCookie)
	return nil
}
