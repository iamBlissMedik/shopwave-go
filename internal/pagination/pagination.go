// Package pagination gives every list endpoint the same request params and
// response shape: { "data": [...], "meta": {...} }.
package pagination

// Params is a 1-based page request.
type Params struct {
	Page  int
	Limit int
}

// Offset converts the page into a SQL OFFSET.
func (p Params) Offset() int { return (p.Page - 1) * p.Limit }

type Meta struct {
	Page        int  `json:"page"`
	Limit       int  `json:"limit"`
	Total       int  `json:"total"`
	TotalPages  int  `json:"totalPages"`
	HasNextPage bool `json:"hasNextPage"`
	HasPrevPage bool `json:"hasPrevPage"`
}

type Page[T any] struct {
	Data []T  `json:"data"`
	Meta Meta `json:"meta"`
}

// New wraps one page of items with its metadata. totalPages is at least 1 so
// clients can always render "page 1 of 1" for an empty list.
func New[T any](items []T, total int, p Params) Page[T] {
	if items == nil {
		items = []T{} // encode as [] rather than null
	}
	totalPages := max(1, (total+p.Limit-1)/p.Limit)
	return Page[T]{
		Data: items,
		Meta: Meta{
			Page:        p.Page,
			Limit:       p.Limit,
			Total:       total,
			TotalPages:  totalPages,
			HasNextPage: p.Page < totalPages,
			HasPrevPage: p.Page > 1,
		},
	}
}
