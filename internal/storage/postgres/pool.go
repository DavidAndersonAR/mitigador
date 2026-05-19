package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool builds a pgxpool from the given DSN with reasonable clamps.
// maxConns < 2 is bumped to 2; minConns < 1 is bumped to 1.
// The pool is health-checked with Ping before being returned.
func NewPool(ctx context.Context, dsn string, maxConns, minConns int32) (*pgxpool.Pool, error) {
	if dsn == "" {
		return nil, fmt.Errorf("postgres: empty DSN")
	}
	if maxConns < 2 {
		maxConns = 2
	}
	if minConns < 1 {
		minConns = 1
	}
	if minConns > maxConns {
		minConns = maxConns
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DSN: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = minConns

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: new pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}
