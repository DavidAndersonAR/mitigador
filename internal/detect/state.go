package detect

import (
	"math/rand"
	"net/netip"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// CooldownAfterEnd is the minimum quiet period after StateEnded before the same
// (host, vector) can re-trigger StateStarted (D-16).
const CooldownAfterEnd = 60 * time.Second

// UpdateAfterDuration is the elapsed time threshold for emitting a single
// StateUpdated during an active incident, regardless of peak growth (D-15).
const UpdateAfterDuration = 5 * time.Minute

// Key identifies a per-(host_ip, vector) state machine entry.
// Exported so the engine and tests can construct keys directly.
type Key struct {
	HostIP netip.Addr
	Vector Vector
}

// ExportKey is a convenience constructor for Key used in tests.
func ExportKey(ip netip.Addr, v Vector) Key {
	return Key{HostIP: ip, Vector: v}
}

// Input is the per-tick observation handed to the state machine.
// Exported so tests can construct inputs without internal coupling.
type Input struct {
	Now              time.Time
	CurrentPps       uint64
	CurrentBps       uint64
	AvgPpsLastWindow uint64
	AvgBpsLastWindow uint64
	Violating        bool    // avgPps > threshold.PPS OR avgBps > threshold.BPS this tick
	DurationViolated int     // consecutive seconds violating (only meaningful when Violating)
	Hostgroup        string
	Confidence       float64
}

type machineState int

const (
	stateIdle machineState = iota
	stateActive
	stateCooldown
)

type incident struct {
	id               string
	startedAt        time.Time
	initialPeakPps   uint64
	initialPeakBps   uint64
	peakPps          uint64
	peakBps          uint64
	updateEmitted    bool
	noViolationTicks int // consecutive non-violating ticks since last violation (grace counter)
}

type machineEntry struct {
	state         machineState
	inc           *incident
	cooldownUntil time.Time
}

// StateMachine holds per-(host_ip, vector) state.
// Safe for concurrent use via an internal mutex.
//
// In production the engine calls Step from a single goroutine (the 1Hz ticker).
// Tests that call Step directly must also do so from a single goroutine.
type StateMachine struct {
	mu  sync.Mutex
	m   map[Key]*machineEntry
	rnd *rand.Rand
}

// NewStateMachine creates a ready-to-use StateMachine.
func NewStateMachine() *StateMachine {
	return &StateMachine{
		m:   make(map[Key]*machineEntry),
		rnd: rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // ULID entropy, not crypto
	}
}

func (sm *StateMachine) newULID(now time.Time) string {
	ms := ulid.Timestamp(now)
	id, _ := ulid.New(ms, sm.rnd)
	return id.String()
}

// Step advances the state machine for one (key, input) pair and returns an
// AttackEvent to emit, if any. Returns nil when no state transition fires.
func (sm *StateMachine) Step(k Key, in Input, t Threshold) *AttackEvent {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	e, ok := sm.m[k]
	if !ok {
		e = &machineEntry{state: stateIdle}
		sm.m[k] = e
	}

	graceTicks := t.GraceSec
	if graceTicks <= 0 {
		graceTicks = 60
	}

	switch e.state {
	case stateIdle:
		if !in.Violating {
			return nil
		}
		if in.DurationViolated < t.MinWindowSec {
			return nil
		}
		// Transition idle → active. Emit STARTED.
		e.state = stateActive
		e.inc = &incident{
			id:             sm.newULID(in.Now),
			startedAt:      in.Now,
			initialPeakPps: in.CurrentPps,
			initialPeakBps: in.CurrentBps,
			peakPps:        in.CurrentPps,
			peakBps:        in.CurrentBps,
		}
		return &AttackEvent{
			IncidentID: e.inc.id,
			State:      StateStarted,
			HostIP:     k.HostIP,
			Vector:     k.Vector,
			Hostgroup:  in.Hostgroup,
			Pps:        in.CurrentPps,
			Bps:        in.CurrentBps,
			PeakPps:    in.CurrentPps,
			PeakBps:    in.CurrentBps,
			Confidence: in.Confidence,
			StartedAt:  in.Now,
			Now:        in.Now,
		}

	case stateActive:
		// Always update running peaks.
		if in.CurrentPps > e.inc.peakPps {
			e.inc.peakPps = in.CurrentPps
		}
		if in.CurrentBps > e.inc.peakBps {
			e.inc.peakBps = in.CurrentBps
		}

		if !in.Violating {
			// Increment the consecutive no-violation tick counter.
			e.inc.noViolationTicks++
			if e.inc.noViolationTicks >= graceTicks {
				// Transition active → cooldown. Emit ENDED.
				ev := &AttackEvent{
					IncidentID: e.inc.id,
					State:      StateEnded,
					HostIP:     k.HostIP,
					Vector:     k.Vector,
					Hostgroup:  in.Hostgroup,
					Pps:        in.CurrentPps,
					Bps:        in.CurrentBps,
					PeakPps:    e.inc.peakPps,
					PeakBps:    e.inc.peakBps,
					Confidence: in.Confidence,
					StartedAt:  e.inc.startedAt,
					EndedAt:    in.Now,
					Now:        in.Now,
				}
				e.state = stateCooldown
				e.cooldownUntil = in.Now.Add(CooldownAfterEnd)
				e.inc = nil
				return ev
			}
			return nil
		}

		// Violating — reset the no-violation streak.
		e.inc.noViolationTicks = 0

		// Maybe emit a single UPDATED (D-15).
		if !e.inc.updateEmitted {
			duration := in.Now.Sub(e.inc.startedAt)
			peakDoubled := e.inc.peakPps > 2*e.inc.initialPeakPps ||
				e.inc.peakBps > 2*e.inc.initialPeakBps
			if duration >= UpdateAfterDuration || peakDoubled {
				e.inc.updateEmitted = true
				return &AttackEvent{
					IncidentID: e.inc.id,
					State:      StateUpdated,
					HostIP:     k.HostIP,
					Vector:     k.Vector,
					Hostgroup:  in.Hostgroup,
					Pps:        in.CurrentPps,
					Bps:        in.CurrentBps,
					PeakPps:    e.inc.peakPps,
					PeakBps:    e.inc.peakBps,
					Confidence: in.Confidence,
					StartedAt:  e.inc.startedAt,
					Now:        in.Now,
				}
			}
		}
		return nil

	case stateCooldown:
		// After cooldown expires, silently return to idle.
		// The caller (engine) will re-evaluate on the next tick.
		if in.Now.After(e.cooldownUntil) {
			e.state = stateIdle
		}
		return nil
	}
	return nil
}
