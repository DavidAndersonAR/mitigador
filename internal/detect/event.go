// Package detect runs the 1Hz detection tick and emits AttackEvent.
//
// Filled in plan 01-07; see .planning/phases/01-observation-spine/01-07-PLAN.md.
package detect

import (
	"net/netip"
	"time"
)

// State represents the lifecycle phase of a detection incident.
type State string

const (
	StateStarted State = "started"
	StateUpdated State = "updated"
	StateEnded   State = "ended"
)

// Vector classifies the dominant attack traffic type.
type Vector string

const (
	VectorUDPFlood  Vector = "udp_flood"
	VectorICMPFlood Vector = "icmp_flood"
)

// IsP1Vector returns true for the Phase 1 primary vectors (UDP and ICMP flood).
// Hosts whose dominant proto maps to no P1 vector are skipped by the engine.
func IsP1Vector(v Vector) bool {
	return v == VectorUDPFlood || v == VectorICMPFlood
}

// AttackEvent is the contract on the detector→alert/incident bus.
// IncidentID is a ULID that is stable across started/updated/ended events
// for the same incident.
type AttackEvent struct {
	IncidentID string     // ULID, stable across started/updated/ended for one incident
	State      State
	HostIP     netip.Addr
	Vector     Vector
	Hostgroup  string     // hostgroup name (display only)
	Pps        uint64     // current 1s pps
	Bps        uint64     // current 1s bps
	PeakPps    uint64
	PeakBps    uint64
	Confidence float64    // 0..1
	StartedAt  time.Time
	EndedAt    time.Time  // zero when not ended
	Now        time.Time  // event timestamp
}
