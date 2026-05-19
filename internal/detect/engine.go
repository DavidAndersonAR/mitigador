package detect

import (
	"context"
	"log/slog"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/mitigador/mitigador/internal/aggregate"
)

// Engine is the 1Hz detection loop.
//
// Architecture:
//   - Run(ctx) ticks at 1Hz using time.Ticker.
//   - Each tick: advance the store's sliding window via store.Tick(sec), then
//     evaluate every active host against the threshold catalog.
//   - Hosts not matched by any hostgroup are skipped (DETE-01 / D-10).
//   - A non-blocking send is used on the out channel; excess events increment
//     the Dropped counter (backpressure is the caller's responsibility).
//
// Thread safety: streak map and stateMachine are only accessed from the single
// goroutine that calls Run (or the test-only Tick method). Do not call Tick
// concurrently with Run or other Tick calls.
type Engine struct {
	store   *aggregate.Store
	catalog *Catalog
	out     chan<- AttackEvent
	sm      *StateMachine
	dropped atomic.Uint64
	// streak tracks consecutive violating-second counts per (host, vector).
	// Written and read only from the single ticker goroutine.
	streak map[Key]int
}

// NewEngine wires the components together.
func NewEngine(store *aggregate.Store, catalog *Catalog, out chan<- AttackEvent) *Engine {
	return &Engine{
		store:   store,
		catalog: catalog,
		out:     out,
		sm:      NewStateMachine(),
		streak:  make(map[Key]int),
	}
}

// Dropped returns the number of AttackEvents dropped because the out channel
// was full. In steady-state this should be zero; a rising counter means the
// downstream consumer (incident recorder / alert bus) is falling behind.
func (e *Engine) Dropped() uint64 {
	return e.dropped.Load()
}

// Run blocks until ctx is canceled, ticking at 1 Hz.
// It returns ctx.Err() on cancellation.
func (e *Engine) Run(ctx context.Context) error {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-t.C:
			e.Tick(now)
		}
	}
}

// Tick is exported for deterministic testing — call once per simulated second.
// In production, Run calls Tick from the ticker goroutine; tests call it directly.
// Must be called from a single goroutine only (not concurrency-safe by design).
func (e *Engine) Tick(now time.Time) {
	sec := now.Unix()
	e.store.Tick(sec)
	hosts := e.store.ActiveHosts(sec)
	for _, h := range hosts {
		e.evalHost(h.IP, sec, now)
	}
}

// evalHost evaluates one host for a single tick.
func (e *Engine) evalHost(ip netip.Addr, sec int64, now time.Time) {
	thresholds := e.catalog.Lookup(ip)
	if len(thresholds) == 0 {
		// DETE-01 + D-10: no detection without a configured threshold.
		return
	}

	// Snapshot the widest window any threshold might need.
	maxWindow := 0
	for _, t := range thresholds {
		if t.MinWindowSec > maxWindow {
			maxWindow = t.MinWindowSec
		}
	}
	if maxWindow < 1 {
		maxWindow = 5
	}
	buckets := e.store.Snapshot(ip, sec, maxWindow)
	if len(buckets) == 0 {
		return
	}

	// Classify the dominant vector for this window.
	vec := Classify(buckets)
	if vec == "" {
		// No P1 vector (UDP/ICMP) dominates — skip this tick.
		return
	}

	// Look up the threshold for the specific (ip, vector) pair.
	t, ok := e.catalog.LookupByVector(ip, vec)
	if !ok {
		// No threshold configured for this vector on this host.
		return
	}

	// Average pps/bps over the threshold's min_window_sec.
	n := t.MinWindowSec
	if n > len(buckets) {
		n = len(buckets)
	}
	var sumPps, sumBps uint64
	for i := 0; i < n; i++ {
		sumPps += buckets[i].Pps
		sumBps += buckets[i].Bps
	}
	avgPps := sumPps / uint64(n)
	avgBps := sumBps / uint64(n)
	violating := avgPps > t.PPS || avgBps > t.BPS

	k := Key{HostIP: ip, Vector: vec}
	if violating {
		e.streak[k]++
	} else {
		e.streak[k] = 0
	}

	in := Input{
		Now:              now,
		CurrentPps:       buckets[0].Pps,
		CurrentBps:       buckets[0].Bps,
		AvgPpsLastWindow: avgPps,
		AvgBpsLastWindow: avgBps,
		Violating:        violating,
		DurationViolated: e.streak[k],
		Hostgroup:        t.HostgroupName,
		Confidence:       Confidence(avgPps, avgBps, t, e.streak[k]),
	}

	ev := e.sm.Step(k, in, t)
	if ev == nil {
		return
	}

	select {
	case e.out <- *ev:
	default:
		e.dropped.Add(1)
		slog.Warn("detect: dropped AttackEvent (out channel full)",
			"host_ip", ip.String(),
			"vector", string(vec),
			"incident_id", ev.IncidentID,
		)
	}
}
