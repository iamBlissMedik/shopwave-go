package order

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/pagination"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

// Checkout turns the user's cart into a PAID order.
//
// It runs as one transaction: the order is created AND stock is decremented
// AND the cart is cleared, or nothing happens — no overselling, no partial
// orders. Two safeguards worth calling out:
//
//   - Guarded decrement: each product's stock is reduced with a conditional
//     UPDATE (see Store.DecrementStock), so the last unit can't be sold twice.
//   - Idempotency: with a key, a retried checkout returns the original order
//     instead of charging twice. The (user_id, idempotency_key) unique
//     constraint settles a concurrent double-submit: the loser returns the
//     winner's order.
//
// MVP: payment is assumed to succeed synchronously (see docs/TODO.md).
func (s *Service) Checkout(ctx context.Context, userID string, key *string) (Order, error) {
	if key != nil {
		if order, found, err := s.byKey(ctx, userID, *key); found || err != nil {
			return order, err
		}
	}

	var orderID string
	err := s.store.InTx(ctx, func(st *Store) error {
		lines, err := st.CartLines(ctx, userID)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return apperr.Invalid("Your cart is empty")
		}

		var total int64
		for _, l := range lines {
			ok, err := st.DecrementStock(ctx, l.ProductID, l.Quantity)
			if err != nil {
				return err
			}
			if !ok {
				return apperr.Invalid(fmt.Sprintf("Not enough stock for %q", l.Name))
			}
			total += l.Price * int64(l.Quantity)
		}

		if orderID, err = st.Insert(ctx, userID, total, key, lines); err != nil {
			return err
		}
		return st.ClearCart(ctx, lines[0].CartID)
	})

	if key != nil && database.IsUniqueViolation(err, "orders_user_idempotency_key") {
		// Lost a concurrent double-submit race: return the winning order.
		order, found, err := s.byKey(ctx, userID, *key)
		if err == nil && !found {
			err = apperr.Conflict("This idempotency key was already used")
		}
		return order, err
	}
	if err != nil {
		return Order{}, err
	}
	return s.store.Get(ctx, userID, orderID)
}

func (s *Service) List(ctx context.Context, userID string, p pagination.Params) (pagination.Page[Order], error) {
	return s.store.List(ctx, userID, p)
}

func (s *Service) Get(ctx context.Context, userID, id string) (Order, error) {
	order, err := s.store.Get(ctx, userID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, apperr.NotFound("Order not found")
	}
	return order, err
}

// byKey loads the user's order for an idempotency key, if one exists.
func (s *Service) byKey(ctx context.Context, userID, key string) (Order, bool, error) {
	id, err := s.store.IDByKey(ctx, userID, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, false, nil
	}
	if err != nil {
		return Order{}, false, err
	}
	order, err := s.store.Get(ctx, userID, id)
	return order, err == nil, err
}
