package ingest

import (
	"net/netip"
	"sync"
	"time"
)

// Status reflects the operator-visible exporter state for DASH-05.
type Status string

const (
	StatusOnline  Status = "online"  // last flow < 60s ago
	StatusStale   Status = "stale"   // 60s ≤ last flow < 5min
	StatusOffline Status = "offline" // last flow ≥ 5min or never
)

// Thresholds for status transitions, matching 01-UI-SPEC.md §"View: Exporter Health".
const (
	StaleAfter   = 60 * time.Second
	OfflineAfter = 5 * time.Minute
)

// ExporterHealth is one row in the DASH-05 view.
type ExporterHealth struct {
	SourceIP           netip.Addr
	Type               string
	LastSeen           time.Time // zero value = never
	FlowsPerSec        float64   // approximate; 60s rolling window
	Status             Status
	SampleRateOverride uint32
}

// HealthTracker maintains per-exporter last-seen + 60s arrival rate.
type HealthTracker struct {
	mu    sync.Mutex
	state map[netip.Addr]*exporterState
}

type exporterState struct {
	lastSeen      time.Time
	// buckets: 60 slots of 1s each, indexed by (unix_sec % 60).
	// Each slot holds the number of flows observed in that second.
	buckets       [60]uint32
	lastBucketSec int64
}

// NewHealthTracker creates a HealthTracker.
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{state: make(map[netip.Addr]*exporterState)}
}

// Observe records one flow arrival from the given exporter at `now`.
// Normalizes IPv4-mapped IPv6 to plain IPv4 so the per-exporter state survives
// dual-stack listener address representation differences.
func (h *HealthTracker) Observe(exporter netip.Addr, now time.Time) {
	exporter = exporter.Unmap()
	h.mu.Lock()
	defer h.mu.Unlock()

	es, ok := h.state[exporter]
	if !ok {
		es = &exporterState{}
		h.state[exporter] = es
	}
	es.lastSeen = now

	sec := now.Unix()
	idx := sec % 60

	if sec != es.lastBucketSec {
		// Zero out buckets between lastBucketSec and sec to evict stale counts.
		gap := sec - es.lastBucketSec
		if gap >= 60 {
			for i := range es.buckets {
				es.buckets[i] = 0
			}
		} else {
			for g := int64(1); g <= gap; g++ {
				es.buckets[(es.lastBucketSec+g)%60] = 0
			}
		}
		es.lastBucketSec = sec
	}
	es.buckets[idx]++
}

// Snapshot returns one ExporterHealth per entry in inv.
// An exporter with no observations appears with LastSeen=zero, FlowsPerSec=0, Status=offline.
func (h *HealthTracker) Snapshot(inv *Inventory, now time.Time) []ExporterHealth {
	h.mu.Lock()
	defer h.mu.Unlock()

	exps := inv.All()
	out := make([]ExporterHealth, 0, len(exps))
	for _, e := range exps {
		row := ExporterHealth{
			SourceIP:           e.SourceIP,
			Type:               e.Type,
			SampleRateOverride: e.SampleRateOverride,
		}
		es, ok := h.state[e.SourceIP]
		if !ok {
			row.Status = StatusOffline
			out = append(out, row)
			continue
		}
		row.LastSeen = es.lastSeen
		age := now.Sub(es.lastSeen)
		switch {
		case age < StaleAfter:
			row.Status = StatusOnline
		case age < OfflineAfter:
			row.Status = StatusStale
		default:
			row.Status = StatusOffline
		}
		// Sum of all 60 buckets / 60 = avg flows/sec over the last minute.
		var sum uint32
		for _, b := range es.buckets {
			sum += b
		}
		row.FlowsPerSec = float64(sum) / 60.0
		out = append(out, row)
	}
	return out
}
