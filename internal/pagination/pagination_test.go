package pagination

import "testing"

func TestNew(t *testing.T) {
	tests := []struct {
		name               string
		total, page, limit int
		wantPages          int
		wantNext, wantPrev bool
	}{
		{"empty list still has one page", 0, 1, 10, 1, false, false},
		{"exact fit", 20, 1, 10, 2, true, false},
		{"partial last page", 21, 3, 10, 3, false, true},
		{"middle page", 50, 2, 10, 5, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New[int](nil, tt.total, Params{Page: tt.page, Limit: tt.limit})
			if got.Data == nil {
				t.Error("Data is nil; want empty slice so it encodes as []")
			}
			m := got.Meta
			if m.TotalPages != tt.wantPages || m.HasNextPage != tt.wantNext || m.HasPrevPage != tt.wantPrev {
				t.Errorf("meta = %+v, want pages=%d next=%v prev=%v", m, tt.wantPages, tt.wantNext, tt.wantPrev)
			}
		})
	}
}

func TestOffset(t *testing.T) {
	if got := (Params{Page: 3, Limit: 12}).Offset(); got != 24 {
		t.Errorf("Offset() = %d, want 24", got)
	}
}
