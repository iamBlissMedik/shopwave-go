package httpx

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/pagination"
)

// Query reads typed, bounded values from the URL query string. It collects
// every problem so the client gets all validation errors at once:
//
//	q := httpx.NewQuery(r)
//	page := q.Page(12, 100)
//	sort := q.OneOf("sort", "newest", "newest", "price_asc")
//	if err := q.Err(); err != nil { return err }
type Query struct {
	values url.Values
	errs   apperr.FieldErrors
}

func NewQuery(r *http.Request) *Query {
	return &Query{values: r.URL.Query(), errs: apperr.FieldErrors{}}
}

// String returns the trimmed value, or "" when absent.
func (q *Query) String(key string) string { return strings.TrimSpace(q.values.Get(key)) }

// Int returns an integer in [min, max], or def when absent.
func (q *Query) Int(key string, def, min, max int) int {
	if n := q.OptionalInt(key, min, max); n != nil {
		return *n
	}
	return def
}

// OptionalInt returns an integer in [min, max], or nil when absent.
func (q *Query) OptionalInt(key string, min, max int) *int {
	raw := q.String(key)
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	switch {
	case err != nil:
		q.errs.Add(key, "must be an integer")
	case n < min:
		q.errs.Add(key, "must be at least "+strconv.Itoa(min))
	case n > max:
		q.errs.Add(key, "must be at most "+strconv.Itoa(max))
	default:
		return &n
	}
	return nil
}

// OneOf returns the value if it is one of allowed, or def when absent.
func (q *Query) OneOf(key, def string, allowed ...string) string {
	raw := q.String(key)
	if raw == "" {
		return def
	}
	if !slices.Contains(allowed, raw) {
		q.errs.Add(key, "must be one of: "+strings.Join(allowed, ", "))
		return def
	}
	return raw
}

// Page reads ?page and ?limit with the given default and maximum page size.
func (q *Query) Page(defaultLimit, maxLimit int) pagination.Params {
	return pagination.Params{
		Page:  q.Int("page", 1, 1, 1_000_000),
		Limit: q.Int("limit", defaultLimit, 1, maxLimit),
	}
}

// Err returns a validation error if any value was invalid.
func (q *Query) Err() error {
	if len(q.errs) > 0 {
		return apperr.Validation(q.errs)
	}
	return nil
}
