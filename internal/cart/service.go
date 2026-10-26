package cart

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/product"
)

// Cart is the response shape for every cart endpoint. Money is in cents.
type Cart struct {
	Items     []Line `json:"items"`
	Subtotal  int64  `json:"subtotal"`
	ItemCount int    `json:"itemCount"`
}

type AddInput struct {
	ProductID string `json:"productId" validate:"required,uuid"`
	Quantity  *int   `json:"quantity" validate:"min=1,max=99"` // defaults to 1
}

func (in *AddInput) Normalize() {
	if in.Quantity == nil {
		one := 1
		in.Quantity = &one
	}
}

type UpdateInput struct {
	Quantity int `json:"quantity" validate:"min=1,max=99"`
}

// Service implements the cart rules. Stock checks here are advisory (adding
// to a cart never reserves stock); checkout is the authoritative guard.
type Service struct {
	store    *Store
	products *product.Store
}

func NewService(store *Store, products *product.Store) *Service {
	return &Service{store: store, products: products}
}

func (s *Service) Get(ctx context.Context, o Owner) (Cart, error) {
	lines, err := s.store.Lines(ctx, o)
	if err != nil {
		return Cart{}, err
	}
	c := Cart{Items: lines}
	for _, l := range lines {
		c.Subtotal += l.LineTotal
		c.ItemCount += l.Quantity
	}
	return c, nil
}

func (s *Service) Add(ctx context.Context, o Owner, in AddInput) (Cart, error) {
	p, err := s.product(ctx, in.ProductID)
	if err != nil {
		return Cart{}, err
	}
	current, err := s.store.Quantity(ctx, o, p.ID)
	if err != nil {
		return Cart{}, err
	}
	if current+*in.Quantity > p.Stock {
		return Cart{}, outOfStock(p)
	}
	cartID, err := s.store.Ensure(ctx, o)
	if err != nil {
		return Cart{}, err
	}
	if err := s.store.Add(ctx, cartID, p.ID, *in.Quantity); err != nil {
		return Cart{}, err
	}
	return s.Get(ctx, o)
}

func (s *Service) Update(ctx context.Context, o Owner, productID string, in UpdateInput) (Cart, error) {
	p, err := s.product(ctx, productID)
	if err != nil {
		return Cart{}, err
	}
	if in.Quantity > p.Stock {
		return Cart{}, outOfStock(p)
	}
	found, err := s.store.SetQuantity(ctx, o, productID, in.Quantity)
	if err != nil {
		return Cart{}, err
	}
	if !found {
		return Cart{}, errNotInCart
	}
	return s.Get(ctx, o)
}

func (s *Service) Remove(ctx context.Context, o Owner, productID string) (Cart, error) {
	found, err := s.store.Remove(ctx, o, productID)
	if err != nil {
		return Cart{}, err
	}
	if !found {
		return Cart{}, errNotInCart
	}
	return s.Get(ctx, o)
}

func (s *Service) Clear(ctx context.Context, o Owner) (Cart, error) {
	if err := s.store.Clear(ctx, o); err != nil {
		return Cart{}, err
	}
	return s.Get(ctx, o)
}

// MergeGuestCart folds a guest cart into the user's cart and deletes it, in
// one transaction so items can never end up stranded in both carts.
// Implements auth.GuestCartMerger.
func (s *Service) MergeGuestCart(ctx context.Context, userID, sessionID string) error {
	return s.store.InTx(ctx, func(st *Store) error {
		guestID, err := st.SessionCartID(ctx, sessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // nothing to merge
		}
		if err != nil {
			return err
		}
		userCartID, err := st.Ensure(ctx, Owner{UserID: userID})
		if err != nil {
			return err
		}
		if err := st.MergeInto(ctx, guestID, userCartID); err != nil {
			return err
		}
		return st.Delete(ctx, guestID)
	})
}

var errNotInCart = apperr.NotFound("Item is not in your cart")

// product loads a product that can be put in a cart (exists and is active).
func (s *Service) product(ctx context.Context, id string) (product.Product, error) {
	p, err := s.products.Get(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !p.IsActive) {
		return product.Product{}, apperr.NotFound("Product not found")
	}
	return p, err
}

func outOfStock(p product.Product) error {
	return apperr.Invalid(fmt.Sprintf("Only %d unit(s) of %q in stock", p.Stock, p.Name))
}
