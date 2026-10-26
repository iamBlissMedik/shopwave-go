package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/iamBlissMedik/shopwave-go/internal/apperr"
	"github.com/iamBlissMedik/shopwave-go/internal/database"
)

const bcryptCost = 10

// dummyHash is compared against when an email isn't registered, so a login
// for an unknown email takes as long as one with a wrong password (no timing
// side channel for discovering which emails exist).
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equaliser"), bcryptCost)

// Service implements registration, login and "who am I".
type Service struct {
	store  *Store
	tokens Tokens
}

func NewService(store *Store, tokens Tokens) *Service {
	return &Service{store: store, tokens: tokens}
}

type RegisterInput struct {
	Name     string `json:"name" validate:"required,min=2,max=60"`
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=8,max=72"` // bcrypt's 72-byte limit
	// Self-service signup is limited to customers and merchants. Admins are
	// created by the seed or promoted by another admin.
	Role      Role    `json:"role" validate:"oneof=USER MERCHANT"`
	StoreName *string `json:"storeName" validate:"required_if=Role MERCHANT,omitempty,min=2,max=80"`
}

func (in *RegisterInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Role == "" {
		in.Role = RoleUser
	}
	if in.StoreName != nil {
		if s := strings.TrimSpace(*in.StoreName); s != "" && in.Role == RoleMerchant {
			in.StoreName = &s
		} else {
			in.StoreName = nil // only merchants have a store
		}
	}
}

type LoginInput struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

func (in *LoginInput) Normalize() { in.Email = strings.ToLower(strings.TrimSpace(in.Email)) }

// HashPassword returns the bcrypt hash used for stored passwords.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	return string(h), err
}

// Register creates the account and returns it with a session token.
func (s *Service) Register(ctx context.Context, in RegisterInput) (User, string, error) {
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, "", err
	}
	user, err := s.store.Create(ctx, NewAccount{
		Name: in.Name, Email: in.Email, PasswordHash: hash, Role: in.Role, StoreName: in.StoreName,
	})
	if database.IsUniqueViolation(err, "users_email_key") {
		return User{}, "", apperr.Conflict("An account with this email already exists")
	}
	if err != nil {
		return User{}, "", err
	}
	return s.withToken(user)
}

// Login verifies credentials. "No such user" and "wrong password" return the
// same error so the endpoint can't be used to discover registered emails.
func (s *Service) Login(ctx context.Context, in LoginInput) (User, string, error) {
	invalid := apperr.Unauthorized("Invalid email or password")

	user, hash, err := s.store.FindByEmail(ctx, in.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(in.Password))
		return User{}, "", invalid
	}
	if err != nil {
		return User{}, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		return User{}, "", invalid
	}
	return s.withToken(user)
}

func (s *Service) Me(ctx context.Context, userID string) (User, error) {
	user, err := s.store.FindByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, apperr.NotFound("User not found")
	}
	return user, err
}

func (s *Service) withToken(u User) (User, string, error) {
	token, err := s.tokens.Issue(Principal{ID: u.ID, Role: u.Role})
	return u, token, err
}
