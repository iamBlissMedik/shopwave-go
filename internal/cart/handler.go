package cart

import (
	"crypto/rand"
	"net/http"
	"time"

	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
)

const guestCookieTTL = 30 * 24 * time.Hour

// Handler serves /api/cart. Routes run behind auth.Middleware.Optional, so the
// same endpoints serve guests and logged-in customers.
type Handler struct {
	svc        *Service
	cookies    httpx.Cookies
	cookieName string
}

func NewHandler(svc *Service, cookies httpx.Cookies, cookieName string) *Handler {
	return &Handler{svc: svc, cookies: cookies, cookieName: cookieName}
}

// Get: GET /api/cart
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) error {
	c, err := h.svc.Get(r.Context(), h.owner(w, r))
	return writeCart(w, c, err)
}

// Add: POST /api/cart/items
func (h *Handler) Add(w http.ResponseWriter, r *http.Request) error {
	var in AddInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	c, err := h.svc.Add(r.Context(), h.owner(w, r), in)
	return writeCart(w, c, err)
}

// Update: PATCH /api/cart/items/{productId}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) error {
	productID, err := httpx.PathID(r, "productId")
	if err != nil {
		return err
	}
	var in UpdateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	c, err := h.svc.Update(r.Context(), h.owner(w, r), productID, in)
	return writeCart(w, c, err)
}

// Remove: DELETE /api/cart/items/{productId}
func (h *Handler) Remove(w http.ResponseWriter, r *http.Request) error {
	productID, err := httpx.PathID(r, "productId")
	if err != nil {
		return err
	}
	c, err := h.svc.Remove(r.Context(), h.owner(w, r), productID)
	return writeCart(w, c, err)
}

// Clear: DELETE /api/cart
func (h *Handler) Clear(w http.ResponseWriter, r *http.Request) error {
	c, err := h.svc.Clear(r.Context(), h.owner(w, r))
	return writeCart(w, c, err)
}

// writeCart writes {"cart": c}, or returns err for the error handler.
func writeCart(w http.ResponseWriter, c Cart, err error) error {
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"cart": c})
}

// owner resolves whose cart this request targets: the logged-in user, or a
// guest session. A guest without a (sane) session cookie gets a fresh random
// one, so later requests reuse the same cart and it can be merged on login.
func (h *Handler) owner(w http.ResponseWriter, r *http.Request) Owner {
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		return Owner{UserID: p.ID}
	}
	if c, err := r.Cookie(h.cookieName); err == nil && c.Value != "" && len(c.Value) <= 64 {
		return Owner{SessionID: c.Value}
	}
	sid := rand.Text() // 128+ bits of entropy: unguessable
	h.cookies.Set(w, h.cookieName, sid, guestCookieTTL)
	return Owner{SessionID: sid}
}
