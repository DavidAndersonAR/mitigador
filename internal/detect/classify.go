package detect

import "github.com/mitigador/mitigador/internal/aggregate"

// Classify returns the dominant Vector for a window of buckets.
// If UDP fraction of total pps > 0.5 → VectorUDPFlood.
// Else if ICMP fraction of total pps > 0.5 → VectorICMPFlood.
// Else returns "" (no P1 vector matches; the engine skips this host for this tick).
func Classify(buckets []aggregate.Bucket) Vector {
	var totalPps, udpPps, icmpPps uint64
	for _, b := range buckets {
		totalPps += b.Pps
		udpPps += b.PpsUDP
		icmpPps += b.PpsICMP
	}
	if totalPps == 0 {
		return ""
	}
	if 2*udpPps > totalPps {
		return VectorUDPFlood
	}
	if 2*icmpPps > totalPps {
		return VectorICMPFlood
	}
	return ""
}
