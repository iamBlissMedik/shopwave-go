// Package apperr defines transport-agnostic application errors.
//
// Services return these to describe *what* went wrong (not found, forbidden,
// invalid input…) without knowing about HTTP. The HTTP layer (package httpx)
// maps each Kind to a status code, so the same services could later back a
// worker, CLI or gRPC API unchanged.
package apperr

// Kind classifies an error. Each kind maps to exactly one HTTP status.
type Kind int

const (
	KindInvalid      Kind = iota + 1 // 400 — bad input, or a business rule rejected it
	KindUnauthorized                 // 401 — not logged in / bad credentials
	KindForbidden                    // 403 — logged in but not allowed
	KindNotFound                     // 404
	KindConflict                     // 409 — duplicate / conflicting state
	KindRateLimited                  // 429 — too many requests
)

// FieldErrors maps a request field to its validation messages, e.g.
// {"email": ["must be a valid email"]}.
type FieldErrors map[string][]string

func (f FieldErrors) Add(field, msg string) { f[field] = append(f[field], msg) }

// Error is an expected, client-facing failure. Its Message is safe to show to users.
type Error struct {
	Kind    Kind
	Message string
	Fields  FieldErrors // optional per-field validation details
}

func (e *Error) Error() string { return e.Message }

func Invalid(msg string) *Error      { return &Error{Kind: KindInvalid, Message: msg} }
func Unauthorized(msg string) *Error { return &Error{Kind: KindUnauthorized, Message: msg} }
func Forbidden(msg string) *Error    { return &Error{Kind: KindForbidden, Message: msg} }
func NotFound(msg string) *Error     { return &Error{Kind: KindNotFound, Message: msg} }
func Conflict(msg string) *Error     { return &Error{Kind: KindConflict, Message: msg} }
func RateLimited(msg string) *Error  { return &Error{Kind: KindRateLimited, Message: msg} }

// Validation is the standard error for a request that failed field validation.
func Validation(fields FieldErrors) *Error {
	return &Error{Kind: KindInvalid, Message: "Validation failed", Fields: fields}
}
