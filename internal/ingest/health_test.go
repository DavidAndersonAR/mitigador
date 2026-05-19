package ingest

import (
	"net/netip"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// newTestInventoryForHealth builds an inventory for health tests.
func newTestInventoryForHealth(ips ...string) *Inventory {
	inv := &Inventory{
		byIP:       make(map[netip.Addr]*Exporter),
		unknownLim: make(map[netip.Addr]*rate.Limiter),
	}
	for _, ip := range ips {
		addr := netip.MustParseAddr(ip)
		inv.byIP[addr] = &Exporter{ID: 1, SourceIP: addr, Type: "netflow"}
	}
	return inv
}

func TestHealthTracker_Unobserved_IsOffline(t *testing.T) {
	h := NewHealthTracker()
	inv := newTestInventoryForHealth("10.0.0.1")
	now := time.Now()

	rows := h.Snapshot(inv, now)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Status != StatusOffline {
		t.Errorf("expected StatusOffline for never-observed exporter, got %s", rows[0].Status)
	}
	if !rows[0].LastSeen.IsZero() {
		t.Errorf("expected zero LastSeen for never-observed exporter")
	}
}

func TestHealthTracker_Online_AfterObserve(t *testing.T) {
	h := NewHealthTracker()
	inv := newTestInventoryForHealth("10.0.0.1")
	ip := netip.MustParseAddr("10.0.0.1")
	now := time.Now()

	h.Observe(ip, now)
	rows := h.Snapshot(inv, now)

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Status != StatusOnline {
		t.Errorf("expected StatusOnline immediately after Observe, got %s", rows[0].Status)
	}
}

func TestHealthTracker_Stale_After60s(t *testing.T) {
	h := NewHealthTracker()
	inv := newTestInventoryForHealth("10.0.0.1")
	ip := netip.MustParseAddr("10.0.0.1")
	now := time.Now()

	h.Observe(ip, now)
	// 61s later
	rows := h.Snapshot(inv, now.Add(61*time.Second))

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Status != StatusStale {
		t.Errorf("expected StatusStale after 61s, got %s", rows[0].Status)
	}
}

func TestHealthTracker_Offline_After5min(t *testing.T) {
	h := NewHealthTracker()
	inv := newTestInventoryForHealth("10.0.0.1")
	ip := netip.MustParseAddr("10.0.0.1")
	now := time.Now()

	h.Observe(ip, now)
	// 301s later
	rows := h.Snapshot(inv, now.Add(301*time.Second))

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Status != StatusOffline {
		t.Errorf("expected StatusOffline after 301s, got %s", rows[0].Status)
	}
}

func TestHealthTracker_FlowsPerSec(t *testing.T) {
	h := NewHealthTracker()
	inv := newTestInventoryForHealth("10.0.0.1")
	ip := netip.MustParseAddr("10.0.0.1")
	base := time.Unix(time.Now().Unix(), 0) // truncate to second boundary

	// Observe once per second for 60 different seconds.
	for i := 0; i < 60; i++ {
		h.Observe(ip, base.Add(time.Duration(i)*time.Second))
	}

	rows := h.Snapshot(inv, base.Add(59*time.Second))
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	fps := rows[0].FlowsPerSec
	// Should be approximately 1.0 (60 observations over 60 buckets)
	if fps < 0.9 || fps > 1.1 {
		t.Errorf("FlowsPerSec = %f, want ~1.0", fps)
	}
}
