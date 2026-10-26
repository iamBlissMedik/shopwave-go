// Package httpx is the small HTTP toolkit every feature handler builds on:
// error-returning handlers, JSON in/out with validation, query parsing,
// route groups with middleware, and cookies.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/database"
)

// HandlerFunc is an http.HandlerFunc that returns an error. Any error is turned
// into the standard JSON error envelope by WriteError, so handlers just
// `return err` instead of repeating error-response code.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

func (f HandlerFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := f(w, r); err != nil {
		WriteError(w, r, err)
	}
}

// JSON writes v as a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// errorBody is the envelope for every error: { "error": { "message", "details"? } }.
type errorBody struct {
	Error struct {
		Message string             `json:"message"`
		Details apperr.FieldErrors `json:"details,omitempty"`
	} `json:"error"`
}

// WriteError renders err as the standard JSON envelope. Expected errors
// (apperr) keep their message; anything else becomes a generic 500 so internal
// details never leak to clients — the real error is logged instead.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status, body := toResponse(err)
	if status >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "unhandled error", "err", err, "method", r.Method, "path", r.URL.Path)
	}
	_ = JSON(w, status, body)
}

func toResponse(err error) (int, errorBody) {
	var body errorBody
	var appErr *apperr.Error
	var maxBytesErr *http.MaxBytesError

	switch {
	case errors.As(err, &appErr):
		body.Error.Message = appErr.Message
		body.Error.Details = appErr.Fields
		return statusFor(appErr.Kind), body
	case errors.As(err, &maxBytesErr):
		body.Error.Message = "Request body too large"
		return http.StatusRequestEntityTooLarge, body
	// Safety nets for database errors a service didn't translate itself.
	case errors.Is(err, pgx.ErrNoRows):
		body.Error.Message = "Record not found"
		return http.StatusNotFound, body
	case database.IsUniqueViolation(err, ""):
		body.Error.Message = "A record with these details already exists"
		return http.StatusConflict, body
	default:
		body.Error.Message = "Internal server error"
		return http.StatusInternalServerError, body
	}
}

func statusFor(k apperr.Kind) int {
	switch k {
	case apperr.KindInvalid:
		return http.StatusBadRequest
	case apperr.KindUnauthorized:
		return http.StatusUnauthorized
	case apperr.KindForbidden:
		return http.StatusForbidden
	case apperr.KindNotFound:
		return http.StatusNotFound
	case apperr.KindConflict:
		return http.StatusConflict
	case apperr.KindRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
