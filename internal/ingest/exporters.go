package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"
)

// Exporter mirrors a row in the exporters table.
type Exporter struct {
	ID                 int64
	SourceIP           netip.Addr
	Type               string // "netflow" | "ipfix" | "sflow"
	SampleRateOverride uint32
	Description        string
}

// Inventory is the in-memory authoritative view of exporters.
type Inventory struct {
	mu         sync.RWMutex
	byIP       map[netip.Addr]*Exporter
	unknownLim map[netip.Addr]*rate.Limiter
}

// LoadInventory reads exporters from Postgres and returns the populated Inventory.
func LoadInventory(ctx context.Context, pool *pgxpool.Pool) (*Inventory, error) {
	inv := &Inventory{
		byIP:       make(map[netip.Addr]*Exporter),
		unknownLim: make(map[netip.Addr]*rate.Limiter),
	}
	if err := inv.Reload(ctx, pool); err != nil {
		return nil, err
	}
	return inv, nil
}

// Reload re-reads the exporters table; safe to call concurrently with Lookup.
// Used after `mitigador config sync`. Hot-reload deferred (D-09) but this method
// is available for explicit reload via an admin endpoint in Phase 3.
func (i *Inventory) Reload(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id, source_ip::text, type, sample_rate_override, COALESCE(description,'') FROM exporters`)
	if err != nil {
		return fmt.Errorf("inventory: query: %w", err)
	}
	defer rows.Close()

	m := make(map[netip.Addr]*Exporter)
	for rows.Next() {
		var (
			id             int64
			ipText         string
			typ            string
			sampleOverride int
			desc           string
		)
		if err := rows.Scan(&id, &ipText, &typ, &sampleOverride, &desc); err != nil {
			return fmt.Errorf("inventory: scan: %w", err)
		}
		// pgx returns INET as "10.0.0.1/32" — strip the CIDR prefix if present.
		hostOnly := stripCIDR(ipText)
		addr, err := netip.ParseAddr(hostOnly)
		if err != nil {
			slog.Warn("inventory: skip exporter with invalid source_ip", "raw", ipText, "err", err.Error())
			continue
		}
		m[addr] = &Exporter{
			ID:                 id,
			SourceIP:           addr,
			Type:               typ,
			SampleRateOverride: uint32(sampleOverride),
			Description:        desc,
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inventory: rows: %w", err)
	}

	i.mu.Lock()
	i.byIP = m
	i.mu.Unlock()
	return nil
}

// stripCIDR removes the "/mask" suffix from an IP string returned by pgx for INET columns.
// e.g. "10.0.0.1/32" → "10.0.0.1", "2001:db8::1/128" → "2001:db8::1".
func stripCIDR(s string) string {
	for idx := len(s) - 1; idx >= 0; idx-- {
		if s[idx] == '/' {
			return s[:idx]
		}
	}
	return s
}

// Lookup returns the Exporter for a source IP (true) or (nil, false) if unknown.
func (i *Inventory) Lookup(ip netip.Addr) (*Exporter, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	e, ok := i.byIP[ip]
	return e, ok
}

// LogUnknown records a rate-limited (1/min per offending IP) warn log for an unknown exporter.
func (i *Inventory) LogUnknown(ip netip.Addr) {
	i.mu.Lock()
	lim, ok := i.unknownLim[ip]
	if !ok {
		lim = rate.NewLimiter(rate.Every(time.Minute), 1)
		i.unknownLim[ip] = lim
	}
	i.mu.Unlock()

	if lim.Allow() {
		slog.Warn("flow from unknown exporter", "src_ip", ip.String())
	}
}

// AllByType returns a snapshot slice of exporters with the given type.
func (i *Inventory) AllByType(t string) []Exporter {
	i.mu.RLock()
	defer i.mu.RUnlock()
	var out []Exporter
	for _, e := range i.byIP {
		if e.Type == t {
			out = append(out, *e)
		}
	}
	return out
}

// All returns a snapshot slice of every exporter.
func (i *Inventory) All() []Exporter {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]Exporter, 0, len(i.byIP))
	for _, e := range i.byIP {
		out = append(out, *e)
	}
	return out
}
