// Package merchant is the seller dashboard: a merchant's own products (via the
// shared product service), their sales feed, and dashboard stats.
package merchant

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/pagination"
	"github.com/iamBlissMedik/shopwave-go/internal/timeseries"
)

// statsWindowDays is the length of the dashboard revenue chart.
const statsWindowDays = 14

// lowStockThreshold flags active products at or below this many units.
const lowStockThreshold = 5

// Sale is one order line for one of the merchant's products. A customer order
// can span several merchants; each merchant only sees their own lines.
type Sale struct {
	ID           string      `json:"id"`
	OrderID      string      `json:"orderId"`
	Product      SaleProduct `json:"product"`
	Quantity     int         `json:"quantity"`
	Revenue      int64       `json:"revenue"` // cents
	CustomerName string      `json:"customerName"`
	Status       string      `json:"status"`
	CreatedAt    time.Time   `json:"createdAt"`
}

type SaleProduct struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl"`
}

type Stats struct {
	Totals struct {
		TotalProducts  int   `json:"totalProducts"`
		ActiveProducts int   `json:"activeProducts"`
		LowStock       int   `json:"lowStock"`
		UnitsSold      int64 `json:"unitsSold"`
		TotalRevenue   int64 `json:"totalRevenue"`
	} `json:"totals"`
	RevenueByDay []timeseries.Point `json:"revenueByDay"`
	TopProducts  []TopProduct       `json:"topProducts"`
}

type TopProduct struct {
	Name    string `json:"name"`
	Revenue int64  `json:"revenue"`
}

// Store is the SQL for the merchant dashboard.
type Store struct{ db database.DB }

func NewStore(db database.DB) *Store { return &Store{db: db} }

// Sales returns a page of the merchant's order lines, newest first.
func (s *Store) Sales(ctx context.Context, merchantID string, p pagination.Params) (pagination.Page[Sale], error) {
	q := database.ListQuery{
		Select: `oi.id, oi.order_id, p.id, p.name, p.image_url, oi.quantity,
			oi.price * oi.quantity, u.name, o.status, o.created_at`,
		From: `order_items oi
			JOIN products p ON p.id = oi.product_id
			JOIN orders o ON o.id = oi.order_id
			JOIN users u ON u.id = o.user_id`,
		OrderBy: "o.created_at DESC, oi.id",
	}
	q.Filter.Where("p.merchant_id = " + q.Filter.Arg(merchantID))
	return database.Paginate(ctx, s.db, q, p, func(row pgx.Row) (Sale, error) {
		var x Sale
		err := row.Scan(&x.ID, &x.OrderID, &x.Product.ID, &x.Product.Name, &x.Product.ImageURL,
			&x.Quantity, &x.Revenue, &x.CustomerName, &x.Status, &x.CreatedAt)
		return x, err
	})
}

// paidLinesFrom joins a merchant's order lines to PAID orders; $1 = merchant id.
const paidLinesFrom = `
	FROM order_items oi
	JOIN products p ON p.id = oi.product_id
	JOIN orders o ON o.id = oi.order_id
	WHERE p.merchant_id = $1 AND o.status = 'PAID'`

// Stats computes dashboard metrics with bounded SQL aggregates — it never
// loads a merchant's sales history into memory.
func (s *Store) Stats(ctx context.Context, merchantID string, now time.Time) (Stats, error) {
	var st Stats
	t := &st.Totals

	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE is_active),
		       COUNT(*) FILTER (WHERE is_active AND stock <= $2)
		FROM products WHERE merchant_id = $1`, merchantID, lowStockThreshold).
		Scan(&t.TotalProducts, &t.ActiveProducts, &t.LowStock)
	if err != nil {
		return st, err
	}

	err = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(oi.price * oi.quantity), 0), COALESCE(SUM(oi.quantity), 0)`+paidLinesFrom,
		merchantID).Scan(&t.TotalRevenue, &t.UnitsSold)
	if err != nil {
		return st, err
	}

	rows, err := s.db.Query(ctx, `
		SELECT p.name, SUM(oi.price * oi.quantity) AS revenue`+paidLinesFrom+`
		GROUP BY p.id, p.name
		ORDER BY revenue DESC
		LIMIT 5`, merchantID)
	if err != nil {
		return st, err
	}
	if st.TopProducts, err = pgx.CollectRows(rows, pgx.RowToStructByPos[TopProduct]); err != nil {
		return st, err
	}

	daily, err := database.DailyTotals(ctx, s.db, `
		SELECT o.created_at::date AS day, SUM(oi.price * oi.quantity)`+paidLinesFrom+`
		AND o.created_at >= $2
		GROUP BY day`, merchantID, timeseries.Since(statsWindowDays, now))
	if err != nil {
		return st, err
	}
	st.RevenueByDay = timeseries.Daily(daily, statsWindowDays, now)
	return st, nil
}
