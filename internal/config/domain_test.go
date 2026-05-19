package config_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mitigador/mitigador/internal/config"
	pg "github.com/mitigador/mitigador/internal/storage/postgres"
)

// domainPool opens a pool for domain tests and truncates domain tables.
// Skips the test if MITIGADOR_TEST_PG_DSN is not set.
func domainPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MITIGADOR_TEST_PG_DSN not set")
	}
	if err := pg.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pg.NewPool(ctx, dsn, 4, 1)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	// Truncate domain tables in dependency order.
	_, _ = pool.Exec(context.Background(),
		`TRUNCATE alert_channels, thresholds, hostgroups, exporters, whitelist RESTART IDENTITY CASCADE`)
	return pool, pool.Close
}

func TestLoadDomain(t *testing.T) {
	d, err := config.LoadDomain("testdata/domain.yaml")
	if err != nil {
		t.Fatalf("LoadDomain: %v", err)
	}
	if len(d.Exporters) != 2 {
		t.Errorf("exporters: want 2, got %d", len(d.Exporters))
	}
	if len(d.Hostgroups) != 1 {
		t.Errorf("hostgroups: want 1, got %d", len(d.Hostgroups))
	}
	if len(d.Thresholds) != 1 {
		t.Errorf("thresholds: want 1, got %d", len(d.Thresholds))
	}
	if len(d.AlertChannels) != 2 {
		t.Errorf("alert_channels: want 2, got %d", len(d.AlertChannels))
	}
	if len(d.Whitelist) != 1 {
		t.Errorf("whitelist: want 1, got %d", len(d.Whitelist))
	}
	if d.Exporters[0].SourceIP != "10.0.0.1" {
		t.Errorf("first exporter IP: %s", d.Exporters[0].SourceIP)
	}
	if d.Thresholds[0].Hostgroup != "corporate" {
		t.Errorf("threshold hostgroup: %s", d.Thresholds[0].Hostgroup)
	}
}

func TestLoadDomain_MissingFile(t *testing.T) {
	_, err := config.LoadDomain("testdata/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestSync_Fresh(t *testing.T) {
	pool, cleanup := domainPool(t)
	defer cleanup()

	d, err := config.LoadDomain("testdata/domain.yaml")
	if err != nil {
		t.Fatalf("LoadDomain: %v", err)
	}
	diff, err := config.Sync(context.Background(), pool, d)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if diff.Exporters.Added != 2 {
		t.Errorf("exporters added: want 2, got %d", diff.Exporters.Added)
	}
	if diff.Exporters.Updated != 0 || diff.Exporters.Unchanged != 0 {
		t.Errorf("exporters updated/unchanged should be 0: updated=%d unchanged=%d",
			diff.Exporters.Updated, diff.Exporters.Unchanged)
	}
	if diff.Hostgroups.Added != 1 {
		t.Errorf("hostgroups added: want 1, got %d", diff.Hostgroups.Added)
	}
	if diff.Thresholds.Added != 1 {
		t.Errorf("thresholds added: want 1, got %d", diff.Thresholds.Added)
	}
	if diff.AlertChannels.Added != 2 {
		t.Errorf("alert_channels added: want 2, got %d", diff.AlertChannels.Added)
	}
	if diff.Whitelist.Added != 1 {
		t.Errorf("whitelist added: want 1, got %d", diff.Whitelist.Added)
	}
}

func TestSync_Idempotent(t *testing.T) {
	pool, cleanup := domainPool(t)
	defer cleanup()

	d, err := config.LoadDomain("testdata/domain.yaml")
	if err != nil {
		t.Fatalf("LoadDomain: %v", err)
	}
	// First sync.
	if _, err := config.Sync(context.Background(), pool, d); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	// Second sync — all rows already exist, nothing changed.
	diff, err := config.Sync(context.Background(), pool, d)
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if diff.Exporters.Added != 0 || diff.Exporters.Updated != 0 {
		t.Errorf("exporters: want all unchanged, got added=%d updated=%d",
			diff.Exporters.Added, diff.Exporters.Updated)
	}
	if diff.Exporters.Unchanged != 2 {
		t.Errorf("exporters unchanged: want 2, got %d", diff.Exporters.Unchanged)
	}
	if diff.Hostgroups.Unchanged != 1 {
		t.Errorf("hostgroups unchanged: want 1, got %d", diff.Hostgroups.Unchanged)
	}
	if diff.Thresholds.Unchanged != 1 {
		t.Errorf("thresholds unchanged: want 1, got %d", diff.Thresholds.Unchanged)
	}
	if diff.AlertChannels.Unchanged != 2 {
		t.Errorf("alert_channels unchanged: want 2, got %d", diff.AlertChannels.Unchanged)
	}
	if diff.Whitelist.Unchanged != 1 {
		t.Errorf("whitelist unchanged: want 1, got %d", diff.Whitelist.Unchanged)
	}
}

func TestSync_Updated(t *testing.T) {
	pool, cleanup := domainPool(t)
	defer cleanup()

	d, err := config.LoadDomain("testdata/domain.yaml")
	if err != nil {
		t.Fatalf("LoadDomain: %v", err)
	}
	// First sync.
	if _, err := config.Sync(context.Background(), pool, d); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	// Mutate first exporter's description.
	exporters := make([]config.ExporterEntry, len(d.Exporters))
	copy(exporters, d.Exporters)
	exporters[0].Description = "Updated description for router 1"
	d2 := *d
	d2.Exporters = exporters

	diff, err := config.Sync(context.Background(), pool, &d2)
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if diff.Exporters.Updated != 1 {
		t.Errorf("exporters updated: want 1, got %d", diff.Exporters.Updated)
	}
	if diff.Exporters.Unchanged != 1 {
		t.Errorf("exporters unchanged: want 1, got %d", diff.Exporters.Unchanged)
	}
}

func TestSync_NoDeleteOnAbsence(t *testing.T) {
	pool, cleanup := domainPool(t)
	defer cleanup()

	d, err := config.LoadDomain("testdata/domain.yaml")
	if err != nil {
		t.Fatalf("LoadDomain: %v", err)
	}
	// First sync with 2 exporters.
	if _, err := config.Sync(context.Background(), pool, d); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	// Second sync with only 1 exporter — the absent one must remain in DB (P1 no-delete).
	d2 := config.Domain{
		Exporters: []config.ExporterEntry{d.Exporters[0]},
	}
	if _, err := config.Sync(context.Background(), pool, &d2); err != nil {
		t.Fatalf("second Sync: %v", err)
	}

	// Both exporters must still be in DB.
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM exporters`).Scan(&count); err != nil {
		t.Fatalf("count exporters: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 exporters in DB after partial sync, got %d (P1 no-delete violated)", count)
	}
}

func TestSync_NilDomain(t *testing.T) {
	pool, cleanup := domainPool(t)
	defer cleanup()

	_, err := config.Sync(context.Background(), pool, nil)
	if err == nil {
		t.Error("expected error for nil domain, got nil")
	}
}
