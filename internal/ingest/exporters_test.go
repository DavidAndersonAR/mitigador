package ingest

import (
	"bytes"
	"log/slog"
	"net/netip"
	"os"
	"testing"

	"golang.org/x/time/rate"
)

// newTestInventory builds an Inventory directly without Postgres.
func newTestInventory(exporters ...*Exporter) *Inventory {
	inv := &Inventory{
		byIP:       make(map[netip.Addr]*Exporter),
		unknownLim: make(map[netip.Addr]*rate.Limiter),
	}
	for _, e := range exporters {
		inv.byIP[e.SourceIP] = e
	}
	return inv
}

func TestInventory_LookupKnown(t *testing.T) {
	ip := netip.MustParseAddr("10.0.0.1")
	exp := &Exporter{ID: 1, SourceIP: ip, Type: "netflow", SampleRateOverride: 0}
	inv := newTestInventory(exp)

	got, ok := inv.Lookup(ip)
	if !ok {
		t.Fatal("Lookup: expected true for known IP")
	}
	if got.ID != 1 {
		t.Errorf("Lookup: got ID=%d, want 1", got.ID)
	}
}

func TestInventory_LookupUnknown(t *testing.T) {
	inv := newTestInventory()
	_, ok := inv.Lookup(netip.MustParseAddr("10.99.99.99"))
	if ok {
		t.Fatal("Lookup: expected false for unknown IP")
	}
}

func TestInventory_LogUnknown_RateLimited(t *testing.T) {
	inv := newTestInventory()
	ip := netip.MustParseAddr("10.0.0.1")

	// Capture slog output via a buffer handler.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	orig := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(orig)

	for i := 0; i < 100; i++ {
		inv.LogUnknown(ip)
	}

	// Count lines in output — rate limiter should allow at most 1.
	lines := countLines(buf.Bytes())
	if lines > 1 {
		t.Errorf("LogUnknown: expected at most 1 log line for same IP, got %d", lines)
	}
	if lines == 0 {
		t.Errorf("LogUnknown: expected at least 1 log line, got 0")
	}
}

func TestInventory_LogUnknown_PerIP(t *testing.T) {
	inv := newTestInventory()
	ipA := netip.MustParseAddr("10.0.0.1")
	ipB := netip.MustParseAddr("10.0.0.2")

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	orig := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(orig)

	for i := 0; i < 100; i++ {
		inv.LogUnknown(ipA)
		inv.LogUnknown(ipB)
	}

	lines := countLines(buf.Bytes())
	if lines < 2 {
		t.Errorf("LogUnknown: expected at least 2 log lines (one per IP), got %d", lines)
	}
}

func TestInventory_LoadFromDB(t *testing.T) {
	dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MITIGADOR_TEST_PG_DSN not set — skipping integration test")
	}
	// Integration path omitted — real test requires DB.
}

// countLines returns the number of non-empty lines in b.
func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	count := 0
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			count++
		}
	}
	return count
}
