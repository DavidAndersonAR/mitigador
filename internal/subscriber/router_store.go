package subscriber

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Router is the persistent representation of a Mikrotik device.
// The plain password is intentionally exposed on this struct so the
// poller can authenticate against the router; API responses must mask
// it before returning to the client (see handleListMikrotikRouters).
type Router struct {
	ID        int64
	Name      string
	URL       string
	Username  string
	Password  string
	VerifyTLS bool
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrRouterNotFound is returned by Get/Update/Delete when no row matches.
var ErrRouterNotFound = errors.New("subscriber: router not found")

// RouterStore is a thin pgx wrapper over the mikrotik_routers table.
type RouterStore struct {
	pool *pgxpool.Pool
}

// NewRouterStore builds a RouterStore. The pool is shared with the rest of
// the daemon — no connection lifecycle concerns here.
func NewRouterStore(pool *pgxpool.Pool) *RouterStore {
	return &RouterStore{pool: pool}
}

// List returns every router, oldest first. Used by both the poller (every
// refresh cycle) and the management UI.
func (s *RouterStore) List(ctx context.Context) ([]Router, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, url, username, password, verify_tls, enabled, created_at, updated_at
		FROM mikrotik_routers
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("mikrotik_routers: list: %w", err)
	}
	defer rows.Close()
	out := make([]Router, 0, 4)
	for rows.Next() {
		var r Router
		if err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.Username, &r.Password, &r.VerifyTLS, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("mikrotik_routers: scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListEnabled is the hot-path query for the poller — skips disabled rows.
func (s *RouterStore) ListEnabled(ctx context.Context) ([]Router, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, url, username, password, verify_tls, enabled, created_at, updated_at
		FROM mikrotik_routers
		WHERE enabled = TRUE
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("mikrotik_routers: list_enabled: %w", err)
	}
	defer rows.Close()
	out := make([]Router, 0, 4)
	for rows.Next() {
		var r Router
		if err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.Username, &r.Password, &r.VerifyTLS, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Get returns one row by id, or ErrRouterNotFound.
func (s *RouterStore) Get(ctx context.Context, id int64) (Router, error) {
	var r Router
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, url, username, password, verify_tls, enabled, created_at, updated_at
		FROM mikrotik_routers
		WHERE id = $1
	`, id).Scan(&r.ID, &r.Name, &r.URL, &r.Username, &r.Password, &r.VerifyTLS, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Router{}, ErrRouterNotFound
	}
	if err != nil {
		return Router{}, fmt.Errorf("mikrotik_routers: get: %w", err)
	}
	return r, nil
}

// Create inserts and returns the populated row with id + timestamps.
func (s *RouterStore) Create(ctx context.Context, r Router) (Router, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO mikrotik_routers (name, url, username, password, verify_tls, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, r.Name, r.URL, r.Username, r.Password, r.VerifyTLS, r.Enabled).
		Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return Router{}, fmt.Errorf("mikrotik_routers: create: %w", err)
	}
	return r, nil
}

// UpdateFields is the partial-update form used by PATCH. Pass nil for fields
// the operator did not include in the request body. The password is only
// touched when the pointer is non-nil — preserves the existing value on a
// PATCH that just renames the router.
type UpdateFields struct {
	Name      *string
	URL       *string
	Username  *string
	Password  *string
	VerifyTLS *bool
	Enabled   *bool
}

// Update applies the supplied non-nil fields to the row.
func (s *RouterStore) Update(ctx context.Context, id int64, u UpdateFields) (Router, error) {
	const q = `
		UPDATE mikrotik_routers SET
			name       = COALESCE($2, name),
			url        = COALESCE($3, url),
			username   = COALESCE($4, username),
			password   = COALESCE($5, password),
			verify_tls = COALESCE($6, verify_tls),
			enabled    = COALESCE($7, enabled),
			updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, url, username, password, verify_tls, enabled, created_at, updated_at
	`
	var r Router
	err := s.pool.QueryRow(ctx, q,
		id, u.Name, u.URL, u.Username, u.Password, u.VerifyTLS, u.Enabled,
	).Scan(&r.ID, &r.Name, &r.URL, &r.Username, &r.Password, &r.VerifyTLS, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Router{}, ErrRouterNotFound
	}
	if err != nil {
		return Router{}, fmt.Errorf("mikrotik_routers: update: %w", err)
	}
	return r, nil
}

// Delete removes one row. Returns ErrRouterNotFound if no row matched.
func (s *RouterStore) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM mikrotik_routers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("mikrotik_routers: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRouterNotFound
	}
	return nil
}
