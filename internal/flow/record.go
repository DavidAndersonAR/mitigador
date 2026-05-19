// Package flow defines the canonical FlowRecord type shared by ingest and aggregate.
//
// Filled in plan 01-05; see .planning/phases/01-observation-spine/01-05-PLAN.md.
package flow

import "time"

// Proto is the IP protocol number extracted from a flow record.
type Proto uint8

const (
	ProtoICMP  Proto = 1
	ProtoTCP   Proto = 6
	ProtoUDP   Proto = 17
	ProtoICMPv6 Proto = 58
)

// Record is the canonical per-flow record produced by the ingest layer
// and consumed by the aggregate layer.
type Record struct {
	// SrcAddr is the source IP address (v4 or v6).
	SrcAddr string
	// DstAddr is the destination IP address (v4 or v6).
	DstAddr string
	// SrcPort is the layer-4 source port (0 for ICMP).
	SrcPort uint16
	// DstPort is the layer-4 destination port (0 for ICMP).
	DstPort uint16
	// Proto is the IP protocol number.
	Proto Proto
	// Packets is the number of packets in this flow sample.
	Packets uint64
	// Bytes is the number of bytes in this flow sample.
	Bytes uint64
	// Received is the wall-clock time the sample was received by the collector.
	Received time.Time
}
