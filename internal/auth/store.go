package auth

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/database"
)

// Store is the SQL for accounts.
type Store struct{ db database.DB }

func NewStore(db database.DB) *Store { return &Store{db: db} }

const userColumns = `id, name, email, role, store_name`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.StoreName)
	return u, err
}

// NewAccount is the data needed to insert a user.
type NewAccount struct {
	Name         string
	Email        string
	PasswordHash string
	Role         Role
	StoreName    *string
}

// Create inserts a user. A duplicate email surfaces as a unique violation on
// the users_email_key constraint.
func (s *Store) Create(ctx context.Context, a NewAccount) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `
		INSERT INTO users (name, email, password_hash, role, store_name)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+userColumns,
		a.Name, a.Email, a.PasswordHash, a.Role, a.StoreName))
}

// FindByEmail returns the user and their password hash (pgx.ErrNoRows if absent).
func (s *Store) FindByEmail(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash string
	err := s.db.QueryRow(ctx, `SELECT `+userColumns+`, password_hash FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.StoreName, &hash)
	return u, hash, err
}

// FindByID returns the user (pgx.ErrNoRows if absent).
func (s *Store) FindByID(ctx context.Context, id string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
}
