package httpx

import (
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
)

type signup struct {
	Email string `json:"email" validate:"required,email"`
	Name  string `json:"name" validate:"required,min=2"`
}

func (s *signup) Normalize() { s.Email = strings.ToLower(strings.TrimSpace(s.Email)) }

func TestDecode(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantFields apperr.FieldErrors // nil = expect success
		wantMsg    string
	}{
		{name: "valid + normalized", body: `{"email":" A@B.co ","name":"Al"}`},
		{name: "field errors use JSON names", body: `{"email":"nope","name":"A"}`,
			wantFields: apperr.FieldErrors{"email": {"must be a valid email"}, "name": {"must be at least 2 characters"}}},
		{name: "wrong type", body: `{"email":1}`, wantFields: apperr.FieldErrors{"email": {"must be a string"}}},
		{name: "empty body", body: ``, wantMsg: "Request body is required"},
		{name: "malformed", body: `{`, wantMsg: "Malformed JSON body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			var dst signup
			err := Decode(httptest.NewRecorder(), r, &dst)

			if tt.wantFields == nil && tt.wantMsg == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if dst.Email != "a@b.co" {
					t.Errorf("Normalize not applied: email = %q", dst.Email)
				}
				return
			}
			var appErr *apperr.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("got %v, want *apperr.Error", err)
			}
			if tt.wantMsg != "" && appErr.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", appErr.Message, tt.wantMsg)
			}
			if tt.wantFields != nil && !reflect.DeepEqual(appErr.Fields, tt.wantFields) {
				t.Errorf("fields = %v, want %v", appErr.Fields, tt.wantFields)
			}
		})
	}
}

func TestQuery(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=2&limit=500&sort=bogus&min=7", nil)
	q := NewQuery(r)
	page := q.Page(10, 100)
	sort := q.OneOf("sort", "newest", "newest", "price_asc")
	min := q.OptionalInt("min", 0, 100)
	absent := q.OptionalInt("absent", 0, 100)

	if page.Page != 2 || sort != "newest" || *min != 7 || absent != nil {
		t.Errorf("page=%+v sort=%q min=%v absent=%v", page, sort, min, absent)
	}
	var appErr *apperr.Error
	if !errors.As(q.Err(), &appErr) || len(appErr.Fields) != 2 {
		t.Fatalf("want errors for limit and sort, got %v", q.Err())
	}
}
