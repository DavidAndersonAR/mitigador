package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	pg "github.com/mitigador/mitigador/internal/storage/postgres"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MITIGADOR_TEST_PG_DSN not set; skipping integration test")
	}
	return dsn
}

func TestNewPool_EmptyDSN(t *testing.T) {
	_, err := pg.NewPool(context.Background(), "", 16, 2)
	if err == nil {
		t.Fatal("expected error for empty DSN, got nil")
	}
}

func TestNewPool_InvalidDSN(t *testing.T) {
	_, err := pg.NewPool(context.Background(), "not-a-real-dsn", 16, 2)
	if err == nil {
		t.Fatal("expected error for malformed DSN, got nil")
	}
}

func TestNewPool_Ping(t *testing.T) {
	dsn := testDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pg.NewPool(ctx, dsn, 16, 2)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
