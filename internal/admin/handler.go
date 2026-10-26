package admin

import (
	"net/http"

	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/httpx"
)

// Handler serves /api/admin users + stats (ADMIN role only).
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Stats: GET /api/admin/stats
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) error {
	stats, err := h.svc.Stats(r.Context())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, stats)
}

// ListUsers: GET /api/admin/users?page&limit&search&role
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) error {
	q := httpx.NewQuery(r)
	page := q.Page(10, 50)
	f := UserFilter{
		Search: q.String("search"),
		Role:   auth.Role(q.OneOf("role", "", string(auth.RoleUser), string(auth.RoleMerchant), string(auth.RoleAdmin))),
	}
	if err := q.Err(); err != nil {
		return err
	}
	result, err := h.svc.ListUsers(r.Context(), f, page)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, result)
}

// UpdateRole: PATCH /api/admin/users/{id}/role
func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var in UpdateRoleInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	user, err := h.svc.UpdateRole(r.Context(), auth.MustPrincipal(r.Context()).ID, id, in.Role)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"user": user})
}

// DeleteUser: DELETE /api/admin/users/{id}
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	if err := h.svc.DeleteUser(r.Context(), auth.MustPrincipal(r.Context()).ID, id); err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}
