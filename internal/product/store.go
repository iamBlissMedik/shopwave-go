// Package product owns the catalogue: the public storefront endpoints, plus the
// ownership-aware service that the merchant and admin dashboards reuse.
package product

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/pagination"
)

// Product is a catalogue item. Money is in cents.
type Product struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Price       int64     `json:"price"`
	ImageURL    string    `json:"imageUrl"`
	Category    string    `json:"category"`
	Stock       int       `json:"stock"`
	IsActive    bool      `json:"isActive"`
	MerchantID  string    `json:"merchantId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Merchant    Merchant  `json:"merchant"`
}

// Merchant is the lightweight public view of the selling store.
type Merchant struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	StoreName *string `json:"storeName"`
}

// Columns and From select a product joined with its merchant. Other features
// (cart, order) embed them in their own queries and scan with ScanDest, so the
// product shape is defined exactly once.
const (
	Columns = `p.id, p.name, p.slug, p.description, p.price, p.image_url, p.category,
		p.stock, p.is_active, p.merchant_id, p.created_at, p.updated_at,
		m.id, m.name, m.store_name`
	From = `products p JOIN users m ON m.id = p.merchant_id`
)

// ScanDest returns scan destinations for Columns, in order.
func ScanDest(p *Product) []any {
	return []any{
		&p.ID, &p.Name, &p.Slug, &p.Description, &p.Price, &p.ImageURL, &p.Category,
		&p.Stock, &p.IsActive, &p.MerchantID, &p.CreatedAt, &p.UpdatedAt,
		&p.Merchant.ID, &p.Merchant.Name, &p.Merchant.StoreName,
	}
}

func scan(row pgx.Row) (Product, error) {
	var p Product
	err := row.Scan(ScanDest(&p)...)
	return p, err
}

// Store is the SQL for products.
type Store struct{ db database.DB }

func NewStore(db database.DB) *Store { return &Store{db: db} }

// ListFilter combines every catalogue filter; zero values mean "don't filter".
type ListFilter struct {
	Search     string // matches name or description, case-insensitive
	Category   string
	MinPrice   *int // cents
	MaxPrice   *int // cents
	MerchantID string
	Active     *bool
	Sort       string // a key of sortOrders; "" = newest
}

// Sort keys accepted by the public catalogue. p.id breaks ties so pagination
// is stable when many rows share a price/name/timestamp.
var sortOrders = map[string]string{
	"newest":     "p.created_at DESC, p.id",
	"price_asc":  "p.price ASC, p.id",
	"price_desc": "p.price DESC, p.id",
	"name_asc":   "p.name ASC, p.id",
}

// SortKeys lists the valid ListFilter.Sort values (for request validation).
var SortKeys = []string{"newest", "price_asc", "price_desc", "name_asc"}

func (s *Store) List(ctx context.Context, f ListFilter, p pagination.Params) (pagination.Page[Product], error) {
	q := database.ListQuery{Select: Columns, From: From, OrderBy: sortOrders["newest"]}
	if order, ok := sortOrders[f.Sort]; ok {
		q.OrderBy = order
	}
	w := &q.Filter
	if f.Search != "" {
		like := w.Arg("%" + f.Search + "%")
		w.Where("(p.name ILIKE " + like + " OR p.description ILIKE " + like + ")")
	}
	if f.Category != "" {
		w.Where("p.category = " + w.Arg(f.Category))
	}
	if f.MinPrice != nil {
		w.Where("p.price >= " + w.Arg(*f.MinPrice))
	}
	if f.MaxPrice != nil {
		w.Where("p.price <= " + w.Arg(*f.MaxPrice))
	}
	if f.MerchantID != "" {
		w.Where("p.merchant_id = " + w.Arg(f.MerchantID))
	}
	if f.Active != nil {
		w.Where("p.is_active = " + w.Arg(*f.Active))
	}
	return database.Paginate(ctx, s.db, q, p, scan)
}

// Get returns a product regardless of visibility (pgx.ErrNoRows if absent).
func (s *Store) Get(ctx context.Context, id string) (Product, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+Columns+` FROM `+From+` WHERE p.id = $1`, id))
}

// Categories lists the distinct categories of active products.
func (s *Store) Categories(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT category FROM products WHERE is_active ORDER BY category`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// NewProduct is the data needed to insert a product.
type NewProduct struct {
	MerchantID  string
	Name        string
	Slug        string
	Description string
	Price       int64
	ImageURL    string
	Category    string
	Stock       int
}

// Create inserts a product. A taken slug surfaces as a unique violation on
// products_slug_key.
func (s *Store) Create(ctx context.Context, n NewProduct) (Product, error) {
	return scan(s.db.QueryRow(ctx, `
		WITH p AS (
			INSERT INTO products (merchant_id, name, slug, description, price, image_url, category, stock)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING *
		)
		SELECT `+Columns+` FROM p JOIN users m ON m.id = p.merchant_id`,
		n.MerchantID, n.Name, n.Slug, n.Description, n.Price, n.ImageURL, n.Category, n.Stock))
}

// Update applies the non-nil fields of in (PATCH semantics) and returns the
// updated product (pgx.ErrNoRows if absent).
func (s *Store) Update(ctx context.Context, id string, in UpdateInput) (Product, error) {
	return scan(s.db.QueryRow(ctx, `
		WITH p AS (
			UPDATE products SET
				name        = COALESCE($2, name),
				description = COALESCE($3, description),
				price       = COALESCE($4, price),
				category    = COALESCE($5, category),
				stock       = COALESCE($6, stock),
				image_url   = COALESCE($7, image_url),
				is_active   = COALESCE($8, is_active)
			WHERE id = $1
			RETURNING *
		)
		SELECT `+Columns+` FROM p JOIN users m ON m.id = p.merchant_id`,
		id, in.Name, in.Description, in.Price, in.Category, in.Stock, in.ImageURL, in.IsActive))
}
