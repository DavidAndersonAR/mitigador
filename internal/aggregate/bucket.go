// Package aggregate holds the sharded per-host ring-buffer counters (60x1s) used by detect — RAM-only per PERS-03.
//
// Filled in plan 01-06; see .planning/phases/01-observation-spine/01-06-PLAN.md.
package aggregate

// Bucket holds one second's pps/bps with per-proto breakdown for one host.
type Bucket struct {
	Pps      uint64
	Bps      uint64
	PpsUDP   uint64
	BpsUDP   uint64
	PpsICMP  uint64
	BpsICMP  uint64
	PpsOther uint64
	BpsOther uint64
}

// Add merges another bucket into b.
func (b *Bucket) Add(o Bucket) {
	b.Pps += o.Pps
	b.Bps += o.Bps
	b.PpsUDP += o.PpsUDP
	b.BpsUDP += o.BpsUDP
	b.PpsICMP += o.PpsICMP
	b.BpsICMP += o.BpsICMP
	b.PpsOther += o.PpsOther
	b.BpsOther += o.BpsOther
}
