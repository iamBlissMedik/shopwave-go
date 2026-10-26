package timeseries

import (
	"testing"
	"time"
)

func TestDaily(t *testing.T) {
	now := time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC)
	day := func(d int) time.Time { return time.Date(2026, 3, d, 0, 0, 0, 0, time.UTC) }

	got := Daily(map[time.Time]int64{
		day(8):  500,
		day(10): 250,
		day(1):  999, // outside the 3-day window: ignored
	}, 3, now)

	want := []Point{{"2026-03-08", 500}, {"2026-03-09", 0}, {"2026-03-10", 250}}
	if len(got) != len(want) {
		t.Fatalf("got %d points, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("point %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSince(t *testing.T) {
	now := time.Date(2026, 3, 10, 23, 59, 0, 0, time.UTC)
	if got, want := Since(14, now), time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Since = %v, want %v", got, want)
	}
}
