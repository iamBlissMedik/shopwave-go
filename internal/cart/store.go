// Package cart implements shopping carts for both logged-in customers and
// anonymous guests (identified by an httpOnly session cookie). A guest cart is
// merged into the customer's cart when they log in or register.
package cart

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/product"
)

// Owner identifies whose cart an operation targets: exactly one of UserID
// (logged-in customer) or SessionID (guest) is set.
type Owner struct {
	UserID    string
	SessionID string
}

// column and value give the carts column that identifies this owner. The
// column name is a code constant, so it is safe to embed in SQL.
func (o Owner) column() string {
	if o.UserID != "" {
		return "user_id"
	}
	return "session_id"
}

func (o Owner) value() string {
	if o.UserID != "" {
		return o.UserID
	}
	return o.SessionID
}

// Line is one product in a cart. Money is in cents.
type Line struct {
	ProductID string          `json:"productId"`
	Quantity  int             `json:"quantity"`
	Product   product.Product `json:"product"`
	LineTotal int64           `json:"lineTotal"`
}

// Store is the SQL for carts and cart items.
type Store struct{ db database.DB }

func NewStore(db database.DB) *Store { return &Store{db: db} }

// InTx runs fn with a Store bound to a single transaction.
func (s *Store) InTx(ctx context.Context, fn func(*Store) error) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error { return fn(&Store{db: tx}) })
}

// Ensure returns the owner's cart id, creating the cart if needed. The no-op
// DO UPDATE makes RETURNING yield the existing row (and bumps updated_at,
// which marks guest carts as recently used).
func (s *Store) Ensure(ctx context.Context, o Owner) (string, error) {
	var id string
	col := o.column()
	err := s.db.QueryRow(ctx, `
		INSERT INTO carts (`+col+`) VALUES ($1)
		ON CONFLICT (`+col+`) DO UPDATE SET `+col+` = EXCLUDED.`+col+`
		RETURNING id`, o.value()).Scan(&id)
	return id, err
}

// Lines returns the owner's cart lines, oldest first. No cart = no lines.
func (s *Store) Lines(ctx context.Context, o Owner) ([]Line, error) {
	rows, err := s.db.Query(ctx, `
		SELECT ci.quantity, `+product.Columns+`
		FROM carts c
		JOIN cart_items ci ON ci.cart_id = c.id
		JOIN `+product.From+` ON p.id = ci.product_id
		WHERE c.`+o.column()+` = $1
		ORDER BY ci.created_at, ci.id`, o.value())
	if err != nil {
		return nil, err
	}
	return database.CollectRows(rows, func(row pgx.Row) (Line, error) {
		var l Line
		err := row.Scan(append([]any{&l.Quantity}, product.ScanDest(&l.Product)...)...)
		l.ProductID = l.Product.ID
		l.LineTotal = l.Product.Price * int64(l.Quantity)
		return l, err
	})
}

// Quantity returns how many of productID the owner has in their cart (0 if none).
func (s *Store) Quantity(ctx context.Context, o Owner, productID string) (int, error) {
	var qty int
	err := s.db.QueryRow(ctx, `
		SELECT ci.quantity FROM cart_items ci JOIN carts c ON c.id = ci.cart_id
		WHERE c.`+o.column()+` = $1 AND ci.product_id = $2`, o.value(), productID).Scan(&qty)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return qty, err
}

// Add increments the line's quantity, creating it if needed. The increment
// happens in the database, so two concurrent adds both count (no lost update).
func (s *Store) Add(ctx context.Context, cartID, productID string, qty int) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO cart_items (cart_id, product_id, quantity) VALUES ($1, $2, $3)
		ON CONFLICT (cart_id, product_id) DO UPDATE SET quantity = cart_items.quantity + EXCLUDED.quantity`,
		cartID, productID, qty)
	return err
}

// SetQuantity sets an existing line's quantity; false if the line doesn't exist.
func (s *Store) SetQuantity(ctx context.Context, o Owner, productID string, qty int) (bool, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE cart_items ci SET quantity = $3
		FROM carts c
		WHERE ci.cart_id = c.id AND c.`+o.column()+` = $1 AND ci.product_id = $2`,
		o.value(), productID, qty)
	return tag.RowsAffected() > 0, err
}

// Remove deletes a line; false if it wasn't in the cart.
func (s *Store) Remove(ctx context.Context, o Owner, productID string) (bool, error) {
	tag, err := s.db.Exec(ctx, `
		DELETE FROM cart_items ci USING carts c
		WHERE ci.cart_id = c.id AND c.`+o.column()+` = $1 AND ci.product_id = $2`,
		o.value(), productID)
	return tag.RowsAffected() > 0, err
}

// Clear empties the owner's cart.
func (s *Store) Clear(ctx context.Context, o Owner) error {
	_, err := s.db.Exec(ctx, `
		DELETE FROM cart_items ci USING carts c
		WHERE ci.cart_id = c.id AND c.`+o.column()+` = $1`, o.value())
	return err
}

// SessionCartID returns the guest cart for a session (pgx.ErrNoRows if none).
func (s *Store) SessionCartID(ctx context.Context, sessionID string) (string, error) {
	var id string
	err := s.db.QueryRow(ctx, `SELECT id FROM carts WHERE session_id = $1`, sessionID).Scan(&id)
	return id, err
}

// MergeInto copies the lines of cart `from` into cart `to`, summing
// quantities and clamping to current stock. Inactive or sold-out products are
// skipped. One statement, so there's no per-item round trip.
func (s *Store) MergeInto(ctx context.Context, from, to string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO cart_items (cart_id, product_id, quantity)
		SELECT $2, gi.product_id, LEAST(gi.quantity, p.stock)
		FROM cart_items gi JOIN products p ON p.id = gi.product_id
		WHERE gi.cart_id = $1 AND p.is_active AND p.stock > 0
		ON CONFLICT (cart_id, product_id) DO UPDATE SET quantity = LEAST(
			cart_items.quantity + EXCLUDED.quantity,
			(SELECT stock FROM products WHERE id = EXCLUDED.product_id)
		)`, from, to)
	return err
}

// Delete removes a cart and (by cascade) its items.
func (s *Store) Delete(ctx context.Context, cartID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM carts WHERE id = $1`, cartID)
	return err
}
