package admin

import (
	"context"
	"time"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/pagination"
)

// Service holds the admin rules, chiefly: an admin can't change or delete
// their own account (so they can't lock themselves out).
type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

type UpdateRoleInput struct {
	Role auth.Role `json:"role" validate:"required,oneof=USER MERCHANT ADMIN"`
}

var errUserNotFound = apperr.NotFound("User not found")

func (s *Service) ListUsers(ctx context.Context, f UserFilter, p pagination.Params) (pagination.Page[User], error) {
	return s.store.ListUsers(ctx, f, p)
}

// UpdateRole promotes or demotes a user. It takes effect at the user's next
// login, because the role is carried in their session token.
func (s *Service) UpdateRole(ctx context.Context, adminID, userID string, role auth.Role) (User, error) {
	if adminID == userID {
		return User{}, apperr.Invalid("You cannot change your own role")
	}
	found, err := s.store.SetRole(ctx, userID, role)
	if err != nil {
		return User{}, err
	}
	if !found {
		return User{}, errUserNotFound
	}
	return s.store.GetUser(ctx, userID)
}

func (s *Service) DeleteUser(ctx context.Context, adminID, userID string) error {
	if adminID == userID {
		return apperr.Invalid("You cannot delete your own account")
	}
	found, err := s.store.DeleteUser(ctx, userID)
	if database.IsForeignKeyViolation(err) {
		return apperr.Conflict("User has linked orders/products and cannot be deleted")
	}
	if err != nil {
		return err
	}
	if !found {
		return errUserNotFound
	}
	return nil
}

func (s *Service) Stats(ctx context.Context) (Stats, error) {
	return s.store.Stats(ctx, time.Now())
}
