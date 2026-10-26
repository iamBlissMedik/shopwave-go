// Package admin is the platform console: user management and platform-wide
// stats. Catalogue moderation reuses the product handlers (see package server).
package admin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/auth"
	"github.com/iamBlissMedik/shopwave-go/internal/database"
	"github.com/iamBlissMedik/shopwave-go/internal/pagination"
	"github.com/iamBlissMedik/shopwave-go/internal/timeseries"
)

const statsWindowDays = 14

// User is the admin directory view of an account.
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      auth.Role `json:"role"`
	StoreName *string   `json:"storeName"`
	CreatedAt time.Time `json:"createdAt"`
	Count     struct {
		Products int `json:"products"`
		Orders   int `json:"orders"`
	} `json:"_count"`
}

const userColumns = `u.id, u.name, u.email, u.role, u.store_name, u.created_at,
	(SELECT COUNT(*) FROM products WHERE merchant_id = u.id),
	(SELECT COUNT(*) FROM orders WHERE user_id = u.id)`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.StoreName, &u.CreatedAt,
		&u.Count.Products, &u.Count.Orders)
	return u, err
}

type Stats struct {
	Totals struct {
		Users     int   `json:"users"`
		Merchants int   `json:"merchants"`
		Products  int   `json:"products"` // active
		Orders    int   `json:"orders"`
		Revenue   int64 `json:"revenue"` // cents, PAID orders
	} `json:"totals"`
	UsersByRole  []RoleCount        `json:"usersByRole"`
	RevenueByDay []timeseries.Point `json:"revenueByDay"`
}

type RoleCount struct {
	Role  auth.Role `json:"role"`
	Count int       `json:"count"`
}

// Store is the SQL for the admin console.
type Store struct{ db database.DB }

func NewStore(db database.DB) *Store { return &Store{db: db} }

// UserFilter narrows the user directory; zero values mean "don't filter".
type UserFilter struct {
	Search string // name or email, case-insensitive
	Role   auth.Role
}

func (s *Store) ListUsers(ctx context.Context, f UserFilter, p pagination.Params) (pagination.Page[User], error) {
	q := database.ListQuery{Select: userColumns, From: "users u", OrderBy: "u.created_at DESC, u.id"}
	w := &q.Filter
	if f.Role != "" {
		w.Where("u.role = " + w.Arg(f.Role))
	}
	if f.Search != "" {
		like := w.Arg("%" + f.Search + "%")
		w.Where("(u.name ILIKE " + like + " OR u.email ILIKE " + like + ")")
	}
	return database.Paginate(ctx, s.db, q, p, scanUser)
}

// GetUser returns a user (pgx.ErrNoRows if absent).
func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users u WHERE u.id = $1`, id))
}

// SetRole changes a user's role; false if the user doesn't exist.
func (s *Store) SetRole(ctx context.Context, id string, role auth.Role) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE users SET role = $2 WHERE id = $1`, id, role)
	return tag.RowsAffected() > 0, err
}

// DeleteUser deletes a user (cascading to their cart, orders and products);
// false if absent. Fails with a foreign-key violation for a merchant whose
// products appear in orders.
func (s *Store) DeleteUser(ctx context.Context, id string) (bool, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	return tag.RowsAffected() > 0, err
}

// Stats computes the platform dashboard with bounded aggregates.
func (s *Store) Stats(ctx context.Context, now time.Time) (Stats, error) {
	var st Stats
	t := &st.Totals
	err := s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM users),
		       (SELECT COUNT(*) FROM users WHERE role = 'MERCHANT'),
		       (SELECT COUNT(*) FROM products WHERE is_active),
		       (SELECT COUNT(*) FROM orders),
		       (SELECT COALESCE(SUM(total), 0) FROM orders WHERE status = 'PAID')`).
		Scan(&t.Users, &t.Merchants, &t.Products, &t.Orders, &t.Revenue)
	if err != nil {
		return st, err
	}

	rows, err := s.db.Query(ctx, `SELECT role, COUNT(*) FROM users GROUP BY role ORDER BY role`)
	if err != nil {
		return st, err
	}
	if st.UsersByRole, err = pgx.CollectRows(rows, pgx.RowToStructByPos[RoleCount]); err != nil {
		return st, err
	}

	daily, err := database.DailyTotals(ctx, s.db, `
		SELECT created_at::date AS day, SUM(total)
		FROM orders
		WHERE status = 'PAID' AND created_at >= $1
		GROUP BY day`, timeseries.Since(statsWindowDays, now))
	if err != nil {
		return st, err
	}
	st.RevenueByDay = timeseries.Daily(daily, statsWindowDays, now)
	return st, nil
}
