// Package order turns a customer's cart into an order (checkout) and serves
// their order history.
package order

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/pagination"
	"github.com/iamBlissMedik/shopwave-go/internal/product"
)

// Order is a placed order. Money is in cents; Total and each item's Price are
// snapshots taken at checkout, so later price changes never alter history.
type Order struct {
	ID             string    `json:"id"`
	UserID         string    `json:"userId"`
	Total          int64     `json:"total"`
	Status         string    `json:"status"` // PENDING | PAID | CANCELLED
	IdempotencyKey *string   `json:"idempotencyKey"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Items          []Item    `json:"items"`
}

type Item struct {
	ID        string          `json:"id"`
	OrderID   string          `json:"orderId"`
	ProductID string          `json:"productId"`
	Price     int64           `json:"price"` // unit price at purchase time
	Quantity  int             `json:"quantity"`
	Product   product.Product `json:"product"`
}

const (
	orderColumns = `o.id, o.user_id, o.total, o.status, o.idempotency_key, o.created_at, o.updated_at`
	statusPaid   = "PAID"
)

func scanOrder(row pgx.Row) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.UserID, &o.Total, &o.Status, &o.IdempotencyKey, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

// Store is the SQL for orders, including the cart reads/writes checkout needs
// inside its transaction.
type Store struct{ db database.DB }

func NewStore(db database.DB) *Store { return &Store{db: db} }

// InTx runs fn with a Store bound to a single transaction.
func (s *Store) InTx(ctx context.Context, fn func(*Store) error) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error { return fn(&Store{db: tx}) })
}

// IDByKey finds the user's order placed with an idempotency key (pgx.ErrNoRows if none).
func (s *Store) IDByKey(ctx context.Context, userID, key string) (string, error) {
	var id string
	err := s.db.QueryRow(ctx, `SELECT id FROM orders WHERE user_id = $1 AND idempotency_key = $2`,
		userID, key).Scan(&id)
	return id, err
}

// cartLine is a cart line priced at the current product price.
type cartLine struct {
	CartID    string
	ProductID string
	Name      string
	Price     int64
	Quantity  int
}

// CartLines returns the user's cart lines ordered by product id. A consistent
// order means concurrent checkouts lock product rows in the same sequence,
// which prevents deadlocks.
func (s *Store) CartLines(ctx context.Context, userID string) ([]cartLine, error) {
	rows, err := s.db.Query(ctx, `
		SELECT c.id, p.id, p.name, p.price, ci.quantity
		FROM carts c
		JOIN cart_items ci ON ci.cart_id = c.id
		JOIN products p ON p.id = ci.product_id
		WHERE c.user_id = $1
		ORDER BY p.id`, userID)
	if err != nil {
		return nil, err
	}
	return database.CollectRows(rows, func(row pgx.Row) (cartLine, error) {
		var l cartLine
		err := row.Scan(&l.CartID, &l.ProductID, &l.Name, &l.Price, &l.Quantity)
		return l, err
	})
}

// DecrementStock takes qty units of an active product only if enough remain.
// The check and the write are one statement, so two buyers can never both
// take the last unit. Returns false when there isn't enough stock.
func (s *Store) DecrementStock(ctx context.Context, productID string, qty int) (bool, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE products SET stock = stock - $2
		WHERE id = $1 AND is_active AND stock >= $2`, productID, qty)
	return tag.RowsAffected() > 0, err
}

// Insert creates a PAID order and its items (one INSERT for all items).
func (s *Store) Insert(ctx context.Context, userID string, total int64, key *string, lines []cartLine) (string, error) {
	var orderID string
	err := s.db.QueryRow(ctx, `
		INSERT INTO orders (user_id, total, status, idempotency_key)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, total, statusPaid, key).Scan(&orderID)
	if err != nil {
		return "", err
	}

	ids := make([]string, len(lines))
	prices := make([]int64, len(lines))
	qtys := make([]int32, len(lines))
	for i, l := range lines {
		ids[i], prices[i], qtys[i] = l.ProductID, l.Price, int32(l.Quantity)
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO order_items (order_id, product_id, price, quantity)
		SELECT $1, t.product_id::uuid, t.price, t.quantity
		FROM unnest($2::text[], $3::bigint[], $4::int[]) AS t(product_id, price, quantity)`,
		orderID, ids, prices, qtys)
	return orderID, err
}

// ClearCart empties a cart after checkout.
func (s *Store) ClearCart(ctx context.Context, cartID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1`, cartID)
	return err
}

// Get returns one of the user's orders with items (pgx.ErrNoRows if absent or
// not theirs — users can never read each other's orders).
func (s *Store) Get(ctx context.Context, userID, id string) (Order, error) {
	o, err := scanOrder(s.db.QueryRow(ctx,
		`SELECT `+orderColumns+` FROM orders o WHERE o.id = $1 AND o.user_id = $2`, id, userID))
	if err != nil {
		return Order{}, err
	}
	orders := []Order{o}
	if err := s.attachItems(ctx, orders); err != nil {
		return Order{}, err
	}
	return orders[0], nil
}

// List returns a page of the user's orders, newest first, with items.
func (s *Store) List(ctx context.Context, userID string, p pagination.Params) (pagination.Page[Order], error) {
	q := database.ListQuery{Select: orderColumns, From: "orders o", OrderBy: "o.created_at DESC, o.id"}
	q.Filter.Where("o.user_id = " + q.Filter.Arg(userID))
	page, err := database.Paginate(ctx, s.db, q, p, scanOrder)
	if err != nil {
		return page, err
	}
	return page, s.attachItems(ctx, page.Data)
}

// attachItems loads the items for all orders in one query (no N+1).
func (s *Store) attachItems(ctx context.Context, orders []Order) error {
	if len(orders) == 0 {
		return nil
	}
	ids := make([]string, len(orders))
	index := make(map[string]int, len(orders))
	for i := range orders {
		ids[i] = orders[i].ID
		index[orders[i].ID] = i
		orders[i].Items = []Item{}
	}

	rows, err := s.db.Query(ctx, `
		SELECT oi.id, oi.order_id, oi.price, oi.quantity, `+product.Columns+`
		FROM order_items oi
		JOIN `+product.From+` ON p.id = oi.product_id
		WHERE oi.order_id = ANY($1::uuid[])
		ORDER BY oi.id`, ids)
	if err != nil {
		return err
	}
	items, err := database.CollectRows(rows, func(row pgx.Row) (Item, error) {
		var it Item
		err := row.Scan(append([]any{&it.ID, &it.OrderID, &it.Price, &it.Quantity}, product.ScanDest(&it.Product)...)...)
		it.ProductID = it.Product.ID
		return it, err
	})
	if err != nil {
		return err
	}
	for _, it := range items {
		o := &orders[index[it.OrderID]]
		o.Items = append(o.Items, it)
	}
	return nil
}
