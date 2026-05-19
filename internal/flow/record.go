// Package flow defines the canonical FlowRecord type shared by ingest and aggregate.
//
// Filled in plan 01-05; see .planning/phases/01-observation-spine/01-05-PLAN.md.
package flow

import (
	"net/netip"
	"time"
)

// Proto is the L4 protocol classifier used by detection.
// Only the values needed by Phase 1 are defined; Phase 3 adds more.
type Proto uint8

const (
	ProtoOther Proto = 0
	ProtoICMP  Proto = 1
	ProtoTCP   Proto = 6
	ProtoUDP   Proto = 17
)

// Record is the canonical per-flow record shared by ingest and aggregate.
// Bytes and Packets are POST sample-rate expansion — they reflect what
// the router would have seen on the wire, not the sampled count.
type Record struct {
	Exporter   netip.Addr // exporter (router) IP that emitted the flow
	DstIP      netip.Addr // destination host — the key for per-host counters
	SrcIP      netip.Addr // source — informational, P1 doesn't index by source
	Proto      Proto
	Bytes      uint64    // post-sample-rate expansion
	Packets    uint64    // post-sample-rate expansion
	SampleRate uint32    // rate that was applied (0 = unsampled)
	Received   time.Time // when Mitigador received the datagram
}
