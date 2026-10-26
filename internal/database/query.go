package database

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/iamBlissMedik/shopwave-go/internal/pagination"
)

// Filter accumulates AND-ed WHERE conditions with positional arguments, so
// optional list filters can be composed without string-concatenating input.
//
//	var f database.Filter
//	f.Where("p.category = " + f.Arg(category))
//
// Condition text must be written in code; user input only ever goes through Arg.
type Filter struct {
	conds []string
	args  []any
}

// Arg registers a value and returns its placeholder ($1, $2, …).
func (f *Filter) Arg(v any) string {
	f.args = append(f.args, v)
	return "$" + strconv.Itoa(len(f.args))
}

// Where adds a condition. Conditions are joined with AND.
func (f *Filter) Where(cond string) { f.conds = append(f.conds, cond) }

// SQL renders " WHERE a AND b", or "" when there are no conditions.
func (f *Filter) SQL() string {
	if len(f.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(f.conds, " AND ")
}

// Args returns a copy of the collected arguments.
func (f *Filter) Args() []any { return append([]any(nil), f.args...) }

// ListQuery describes a paginated SELECT. Select, From and OrderBy are trusted
// SQL fragments written in code — never user input.
type ListQuery struct {
	Select  string // column list, in the order the scan func expects
	From    string // table + joins
	Filter  Filter
	OrderBy string
}

// Paginate runs the count and the page query for q and returns a standard page.
func Paginate[T any](ctx context.Context, db DB, q ListQuery, p pagination.Params, scan func(pgx.Row) (T, error)) (pagination.Page[T], error) {
	var total int
	countSQL := "SELECT COUNT(*) FROM " + q.From + q.Filter.SQL()
	if err := db.QueryRow(ctx, countSQL, q.Filter.Args()...).Scan(&total); err != nil {
		return pagination.Page[T]{}, fmt.Errorf("count: %w", err)
	}

	f := q.Filter // copy so the caller's filter isn't mutated
	f.args = f.Args()
	pageSQL := fmt.Sprintf("SELECT %s FROM %s%s ORDER BY %s LIMIT %s OFFSET %s",
		q.Select, q.From, f.SQL(), q.OrderBy, f.Arg(p.Limit), f.Arg(p.Offset()))

	rows, err := db.Query(ctx, pageSQL, f.args...)
	if err != nil {
		return pagination.Page[T]{}, fmt.Errorf("list: %w", err)
	}
	items, err := CollectRows(rows, scan)
	if err != nil {
		return pagination.Page[T]{}, err
	}
	return pagination.New(items, total, p), nil
}

// CollectRows scans every row with scan and closes rows. Scan funcs take a
// pgx.Row so the same func serves QueryRow (single) and Query (many).
func CollectRows[T any](rows pgx.Rows, scan func(pgx.Row) (T, error)) ([]T, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (T, error) { return scan(r) })
}
