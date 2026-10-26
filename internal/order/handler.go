package order

import (
	"net/http"

	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
)

// maxIdempotencyKeyLen bounds the client-supplied Idempotency-Key header;
// longer keys are ignored rather than stored.
const maxIdempotencyKeyLen = 200

// Handler serves /api/orders (customer, authenticated).
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Checkout: POST /api/orders/checkout (optional Idempotency-Key header)
func (h *Handler) Checkout(w http.ResponseWriter, r *http.Request) error {
	var key *string
	if k := r.Header.Get("Idempotency-Key"); k != "" && len(k) <= maxIdempotencyKeyLen {
		key = &k
	}
	order, err := h.svc.Checkout(r.Context(), auth.MustPrincipal(r.Context()).ID, key)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, map[string]any{"order": order})
}

// List: GET /api/orders?page&limit
func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	q := httpx.NewQuery(r)
	page := q.Page(10, 50)
	if err := q.Err(); err != nil {
		return err
	}
	result, err := h.svc.List(r.Context(), auth.MustPrincipal(r.Context()).ID, page)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, result)
}

// Get: GET /api/orders/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	order, err := h.svc.Get(r.Context(), auth.MustPrincipal(r.Context()).ID, id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"order": order})
}
