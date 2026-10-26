package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
)

// maxBodyBytes caps JSON request bodies; nothing in this API needs more.
const maxBodyBytes = 10 << 10 // 10 KiB

// validate is shared and safe for concurrent use (it caches struct metadata).
var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	// Report fields by their JSON name ("storeName"), not the Go name ("StoreName").
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	return v
}

// Normalizer is implemented by request types that clean their input (trim,
// lowercase, apply defaults) before validation.
type Normalizer interface{ Normalize() }

// Checker is implemented by request types with rules struct tags can't express
// (e.g. "at least one field"). It runs after tag validation passes.
type Checker interface{ Check() error }

// Decode reads a JSON body into dst, then normalizes and validates it.
// dst must be a pointer to a struct with `validate` tags.
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	return Validate(dst)
}

// Validate normalizes and validates v (see Normalizer, Checker).
func Validate(v any) error {
	if n, ok := v.(Normalizer); ok {
		n.Normalize()
	}
	if err := validate.Struct(v); err != nil {
		var verrs validator.ValidationErrors
		if !errors.As(err, &verrs) {
			return err
		}
		fields := apperr.FieldErrors{}
		for _, fe := range verrs {
			fields.Add(fe.Field(), message(fe))
		}
		return apperr.Validation(fields)
	}
	if c, ok := v.(Checker); ok {
		return c.Check()
	}
	return nil
}

func decodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesErr):
		return err // rendered as 413
	case errors.Is(err, io.EOF):
		return apperr.Invalid("Request body is required")
	case errors.As(err, &typeErr):
		return apperr.Validation(apperr.FieldErrors{typeErr.Field: {"must be a " + typeErr.Type.String()}})
	default:
		return apperr.Invalid("Malformed JSON body")
	}
}

// message turns a validator failure into a short, human-readable message.
func message(fe validator.FieldError) string {
	isString := fe.Kind() == reflect.String
	switch fe.Tag() {
	case "required", "required_if":
		return "is required"
	case "email":
		return "must be a valid email"
	case "url", "http_url":
		return "must be a valid URL"
	case "uuid":
		return "must be a valid id"
	case "oneof":
		return "must be one of: " + strings.ReplaceAll(fe.Param(), " ", ", ")
	case "min":
		if isString {
			return fmt.Sprintf("must be at least %s characters", fe.Param())
		}
		return "must be at least " + fe.Param()
	case "max":
		if isString {
			return fmt.Sprintf("must be at most %s characters", fe.Param())
		}
		return "must be at most " + fe.Param()
	default:
		return "is invalid"
	}
}

// PathID returns the {name} path parameter, validated as a UUID.
func PathID(r *http.Request, name string) (string, error) {
	id := r.PathValue(name)
	if validate.Var(id, "required,uuid") != nil {
		return "", apperr.Validation(apperr.FieldErrors{name: {"must be a valid id"}})
	}
	return id, nil
}
