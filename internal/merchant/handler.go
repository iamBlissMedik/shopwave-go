package merchant

import (
	"net/http"
	"time"

	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
)

// Handler serves the merchant's sales feed and dashboard stats. These are
// plain reads with no business rules, so the handler calls the store directly
// (add a service if rules appear). Product management lives in package product.
type Handler struct{ store *Store }

func NewHandler(store *Store) *Handler { return &Handler{store: store} }

// Stats: GET /api/merchant/stats
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) error {
	stats, err := h.store.Stats(r.Context(), auth.MustPrincipal(r.Context()).ID, time.Now())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, stats)
}

// Sales: GET /api/merchant/sales?page&limit
func (h *Handler) Sales(w http.ResponseWriter, r *http.Request) error {
	q := httpx.NewQuery(r)
	page := q.Page(10, 50)
	if err := q.Err(); err != nil {
		return err
	}
	result, err := h.store.Sales(r.Context(), auth.MustPrincipal(r.Context()).ID, page)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, result)
}
