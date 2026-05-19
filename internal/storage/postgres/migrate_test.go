package postgres_test

import (
	"context"
	"testing"
	"time"

	pg "github.com/mitigador/mitigador/internal/storage/postgres"
)

func TestMigrate_Apply(t *testing.T) {
	dsn := testDSN(t)
	if err := pg.Migrate(dsn); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	// Idempotency: second call must not error.
	if err := pg.Migrate(dsn); err != nil {
		t.Fatalf("second migrate (idempotent): %v", err)
	}
	// Verify all 9 tables exist.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pg.NewPool(ctx, dsn, 4, 1)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()
	expected := []string{"sessions", "users", "exporters", "hostgroups", "thresholds", "alert_channels", "whitelist", "incidents", "attack_updates"}
	for _, table := range expected {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`,
			table).Scan(&exists)
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s not present after Migrate()", table)
		}
	}
}
