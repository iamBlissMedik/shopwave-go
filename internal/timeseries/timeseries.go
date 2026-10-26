// Package timeseries builds gap-free daily chart series for the merchant and
// admin dashboards.
package timeseries

import "time"

const dayLayout = "2006-01-02"

type Point struct {
	Date  string `json:"date"` // YYYY-MM-DD (UTC)
	Value int64  `json:"value"`
}

// Since returns midnight UTC of the first day in a window of `days` calendar
// days ending today. Use it as the lower bound of the SQL query.
func Since(days int, now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
}

// Daily returns one point per day in the window, oldest first, taking values
// from totals (keyed by UTC day) and filling missing days with zero so charts
// get a continuous x-axis.
func Daily(totals map[time.Time]int64, days int, now time.Time) []Point {
	start := Since(days, now)
	byDay := make(map[string]int64, len(totals))
	for day, v := range totals {
		byDay[day.UTC().Format(dayLayout)] += v
	}
	out := make([]Point, days)
	for i := range out {
		key := start.AddDate(0, 0, i).Format(dayLayout)
		out[i] = Point{Date: key, Value: byDay[key]}
	}
	return out
}
