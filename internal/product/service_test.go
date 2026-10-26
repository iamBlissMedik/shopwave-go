package product

import "testing"

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Aurora Wireless Headphones": "aurora-wireless-headphones",
		`Pixel 27" Monitor`:          "pixel-27-monitor",
		"  --Café & Co!! ":           "caf-co",
		"!!!":                        "product",
	}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpdateInputCheck(t *testing.T) {
	if (&UpdateInput{}).Check() == nil {
		t.Error("empty update should be rejected")
	}
	active := false
	if err := (&UpdateInput{IsActive: &active}).Check(); err != nil {
		t.Errorf("update with one field rejected: %v", err)
	}
}
