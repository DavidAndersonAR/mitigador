package detect

import (
	"context"
	"net/netip"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Threshold is one operator-configured detection rule.
// Each row in the thresholds table maps to one Threshold value.
type Threshold struct {
	HostgroupID   int64
	HostgroupName string
	Prefix        netip.Prefix
	Vector        Vector
	PPS           uint64
	BPS           uint64
	MinWindowSec  int
	GraceSec      int
}

// Catalog is the in-memory lookup of hostgroup CIDR → thresholds.
// Sorted by prefix length descending for longest-prefix-match.
type Catalog struct {
	entries []Threshold // sorted by Prefix.Bits() descending
}

// NewCatalogFromThresholds builds a Catalog directly from a slice of Threshold values.
// Used in unit tests to avoid database dependency.
func NewCatalogFromThresholds(thresholds []Threshold) *Catalog {
	c := &Catalog{entries: make([]Threshold, len(thresholds))}
	copy(c.entries, thresholds)
	// Sort descending by prefix length — longest match first.
	sort.Slice(c.entries, func(i, j int) bool {
		bi := c.entries[i].Prefix.Bits()
		bj := c.entries[j].Prefix.Bits()
		if bi != bj {
			return bi > bj
		}
		// Secondary sort by HostgroupName for determinism.
		return c.entries[i].HostgroupName < c.entries[j].HostgroupName
	})
	return c
}

// LoadCatalog reads hostgroups and thresholds from Postgres and builds a Catalog.
func LoadCatalog(ctx context.Context, pool *pgxpool.Pool) (*Catalog, error) {
	const q = `
		SELECT h.id, h.name, h.prefix::text, t.vector, t.pps, t.bps, t.min_window_sec, t.grace_sec
		FROM thresholds t
		JOIN hostgroups h ON h.id = t.hostgroup_id
	`
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ts []Threshold
	for rows.Next() {
		var th Threshold
		var prefixStr string
		var vectorStr string
		if err := rows.Scan(
			&th.HostgroupID,
			&th.HostgroupName,
			&prefixStr,
			&vectorStr,
			&th.PPS,
			&th.BPS,
			&th.MinWindowSec,
			&th.GraceSec,
		); err != nil {
			return nil, err
		}
		prefix, err := netip.ParsePrefix(prefixStr)
		if err != nil {
			return nil, err
		}
		th.Prefix = prefix
		th.Vector = Vector(vectorStr)
		ts = append(ts, th)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return NewCatalogFromThresholds(ts), nil
}

// Lookup returns the thresholds that apply to ip using longest-prefix-match.
// All thresholds for the best-matching hostgroup are returned (one per configured vector).
// Returns an empty slice if no hostgroup contains ip.
func (c *Catalog) Lookup(ip netip.Addr) []Threshold {
	// The entries are sorted longest-prefix first. Find the first matching prefix.
	// Then collect all entries with the same prefix (same hostgroup).
	bestBits := -1
	var out []Threshold
	for _, t := range c.entries {
		if !t.Prefix.Contains(ip) {
			continue
		}
		bits := t.Prefix.Bits()
		if bestBits == -1 {
			// First match — this is the longest matching prefix.
			bestBits = bits
		}
		if bits < bestBits {
			// We've moved past the longest-match group — stop.
			break
		}
		out = append(out, t)
	}
	return out
}

// LookupByVector returns the single threshold (if any) for (ip, vector).
// Uses longest-prefix-match, then filters by vector.
func (c *Catalog) LookupByVector(ip netip.Addr, v Vector) (Threshold, bool) {
	for _, t := range c.Lookup(ip) {
		if t.Vector == v {
			return t, true
		}
	}
	return Threshold{}, false
}
