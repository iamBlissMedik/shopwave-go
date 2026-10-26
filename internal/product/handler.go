package product

import (
	"math"
	"net/http"

	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
)

// Handler serves the public catalogue and the product-management endpoints
// mounted under /api/merchant and /api/admin. Management handlers act as the
// authenticated caller; the service decides what that caller may touch.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ---------------------------------------------------------------------------
// Public catalogue
// ---------------------------------------------------------------------------

// List: GET /api/products?page&limit&search&category&minPrice&maxPrice&sort
func (h *Handler) List(w http.ResponseWriter, r *http.Request) error {
	q := httpx.NewQuery(r)
	page := q.Page(12, 100)
	f := ListFilter{
		Search:   q.String("search"),
		Category: q.String("category"),
		MinPrice: q.OptionalInt("minPrice", 0, math.MaxInt32),
		MaxPrice: q.OptionalInt("maxPrice", 0, math.MaxInt32),
		Sort:     q.OneOf("sort", "newest", SortKeys...),
	}
	if err := q.Err(); err != nil {
		return err
	}
	result, err := h.svc.Catalogue(r.Context(), f, page)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, result)
}

// Get: GET /api/products/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := h.svc.GetPublic(r.Context(), id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"product": p})
}

// Categories: GET /api/products/categories
func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) error {
	categories, err := h.svc.Categories(r.Context())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"categories": categories})
}

// ---------------------------------------------------------------------------
// Management (merchant dashboard + admin console)
// ---------------------------------------------------------------------------

// ListOwn: GET /api/merchant/products?page&limit&search — the caller's whole
// catalogue, active and inactive.
func (h *Handler) ListOwn(w http.ResponseWriter, r *http.Request) error {
	q := httpx.NewQuery(r)
	page := q.Page(12, 100)
	f := ListFilter{MerchantID: auth.MustPrincipal(r.Context()).ID, Search: q.String("search")}
	if err := q.Err(); err != nil {
		return err
	}
	result, err := h.svc.List(r.Context(), f, page)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, result)
}

// ListAll: GET /api/admin/products?page&limit&search&status — every
// merchant's products, for moderation.
func (h *Handler) ListAll(w http.ResponseWriter, r *http.Request) error {
	q := httpx.NewQuery(r)
	page := q.Page(10, 50)
	f := ListFilter{Search: q.String("search")}
	if status := q.OneOf("status", "", "active", "inactive"); status != "" {
		active := status == "active"
		f.Active = &active
	}
	if err := q.Err(); err != nil {
		return err
	}
	result, err := h.svc.List(r.Context(), f, page)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, result)
}

// GetOwned: GET /api/merchant/products/{id}
func (h *Handler) GetOwned(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := h.svc.GetOwned(r.Context(), auth.MustPrincipal(r.Context()), id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"product": p})
}

// Create: POST /api/merchant/products — the caller becomes the owner.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) error {
	var in CreateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, err := h.svc.Create(r.Context(), auth.MustPrincipal(r.Context()), in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, map[string]any{"product": p})
}

// Update: PATCH /api/merchant/products/{id} and /api/admin/products/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in UpdateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, err := h.svc.Update(r.Context(), auth.MustPrincipal(r.Context()), id, in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"product": p})
}

// Delete: DELETE /api/merchant/products/{id} — soft delete (hide).
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := h.svc.Delete(r.Context(), auth.MustPrincipal(r.Context()), id); err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}
