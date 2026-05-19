package aggregate

import (
	"hash/fnv"
	"net/netip"
	"runtime"
	"sort"

	"github.com/mitigador/mitigador/internal/flow"
)

// DefaultNumShards is the recommended shard count: runtime.NumCPU().
// Exported as a variable so tests can override it.
var DefaultNumShards = runtime.NumCPU()

// Store is the sharded per-host counter map.
// All per-host counters live in RAM only (PERS-03); nothing in this package
// writes to disk or any database.
type Store struct {
	shards []*Shard
	n      uint32
}

// New returns a Store with numShards shards.
// If numShards < 1, uses DefaultNumShards; if that is also < 1, uses 1.
func New(numShards int) *Store {
	if numShards < 1 {
		numShards = DefaultNumShards
	}
	if numShards < 1 {
		numShards = 1
	}
	s := &Store{n: uint32(numShards), shards: make([]*Shard, numShards)}
	for i := range s.shards {
		s.shards[i] = newShard()
	}
	return s
}

// shardFor returns the shard that owns ip.
// Uses FNV-1a to distribute hosts uniformly across shards.
func (s *Store) shardFor(ip netip.Addr) *Shard {
	h := fnv.New32a()
	b := ip.As16()
	h.Write(b[:])
	return s.shards[h.Sum32()%s.n]
}

// Update merges one flow.Record into the host's bucket at index sec % WindowSize.
// Safe for concurrent calls from multiple goroutines — each shard is independently locked.
func (s *Store) Update(ip netip.Addr, sec int64, r flow.Record) {
	sh := s.shardFor(ip)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	hr, ok := sh.hosts[ip]
	if !ok {
		hr = &HostRing{}
		sh.hosts[ip] = hr
	}
	if sec > hr.LastSec {
		hr.LastSec = sec
	}

	// Map sec to a ring slot. The modulo handles negative seconds safely.
	idx := int(((sec % WindowSize) + WindowSize) % WindowSize)
	b := &hr.Buckets[idx]
	b.Pps += r.Packets
	b.Bps += r.Bytes
	switch r.Proto {
	case flow.ProtoUDP:
		b.PpsUDP += r.Packets
		b.BpsUDP += r.Bytes
	case flow.ProtoICMP, flow.ProtoICMPv6:
		b.PpsICMP += r.Packets
		b.BpsICMP += r.Bytes
	default:
		b.PpsOther += r.Packets
		b.BpsOther += r.Bytes
	}
}

// Snapshot returns the last window 1s-buckets for ip, newest first (index 0 = now).
// If window > WindowSize it is clamped. Returns nil if ip is unknown.
func (s *Store) Snapshot(ip netip.Addr, now int64, window int) []Bucket {
	if window < 1 {
		return nil
	}
	if window > WindowSize {
		window = WindowSize
	}
	sh := s.shardFor(ip)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	hr, ok := sh.hosts[ip]
	if !ok {
		return nil
	}

	out := make([]Bucket, window)
	for i := 0; i < window; i++ {
		idx := int(((now-int64(i))%WindowSize+WindowSize) % WindowSize)
		out[i] = hr.Buckets[idx]
	}
	return out
}

// HostInfo is a lightweight view for ActiveHosts.
type HostInfo struct {
	IP      netip.Addr
	LastSec int64
}

// TopEntry is one row of the Top(N) result: a host's totals over the window
// plus its dominant L4 protocol. Returned by Store.Top.
type TopEntry struct {
	IP            netip.Addr
	Bps           uint64 // total bytes-per-second summed across the window
	Pps           uint64 // total packets-per-second summed across the window
	DominantProto string // "udp", "icmp", or "other" — highest BpsXxx; tie-break udp > icmp > other
}

// Tick is called once per second by the detector.
// For each host, it zeros the bucket at index (now+1) % WindowSize so that when
// Update writes into that slot during the next second the old data is gone
// (sliding-window semantics by overwrite-next-slot).
// It also cold-evicts hosts not seen within WindowSize seconds.
func (s *Store) Tick(now int64) {
	nextIdx := int(((now+1)%WindowSize + WindowSize) % WindowSize)
	for _, sh := range s.shards {
		sh.mu.Lock()
		for ip, hr := range sh.hosts {
			if now-hr.LastSec > WindowSize {
				delete(sh.hosts, ip)
				continue
			}
			hr.Buckets[nextIdx] = Bucket{}
		}
		sh.mu.Unlock()
	}
}

// ActiveHosts returns one HostInfo per host whose LastSec is within WindowSize of now.
// Used by the detection tick to know which hosts to evaluate.
func (s *Store) ActiveHosts(now int64) []HostInfo {
	var out []HostInfo
	for _, sh := range s.shards {
		sh.mu.Lock()
		for ip, hr := range sh.hosts {
			if now-hr.LastSec <= WindowSize {
				out = append(out, HostInfo{IP: ip, LastSec: hr.LastSec})
			}
		}
		sh.mu.Unlock()
	}
	return out
}

// Top returns up to n hosts ranked by total Bps over the current WindowSize seconds, descending.
//
// Implementation notes:
//   - Walks every shard under its own lock (same pattern as ActiveHosts).
//     For each active host (LastSec within WindowSize), it sums the 60 buckets directly
//     from the per-host ring without copying — Snapshot would allocate per host and
//     blow the heap if many hosts are active.
//   - Hosts with Bps==0 across the entire window are excluded.
//   - Deterministic ordering: primary key Bps desc, secondary key IP string asc.
//   - DominantProto: pick the proto with the highest summed BpsXxx; tie-break udp > icmp > other.
//   - If n <= 0, returns an empty slice.
//   - Safe for concurrent use; the per-shard locks prevent races with Update/Tick.
func (s *Store) Top(now int64, n int) []TopEntry {
	if n <= 0 {
		return []TopEntry{}
	}
	var all []TopEntry
	for _, sh := range s.shards {
		sh.mu.Lock()
		for ip, hr := range sh.hosts {
			if now-hr.LastSec > WindowSize {
				continue
			}
			var totalBps, totalPps uint64
			var udpBps, icmpBps, otherBps uint64
			for i := 0; i < WindowSize; i++ {
				b := hr.Buckets[i]
				totalBps += b.Bps
				totalPps += b.Pps
				udpBps += b.BpsUDP
				icmpBps += b.BpsICMP
				otherBps += b.BpsOther
			}
			if totalBps == 0 {
				continue
			}
			proto := "udp"
			best := udpBps
			if icmpBps > best {
				proto = "icmp"
				best = icmpBps
			}
			if otherBps > best {
				proto = "other"
			}
			all = append(all, TopEntry{
				IP:            ip,
				Bps:           totalBps,
				Pps:           totalPps,
				DominantProto: proto,
			})
		}
		sh.mu.Unlock()
	}
	// Sort: Bps desc, then IP string asc (deterministic for ties).
	sort.Slice(all, func(i, j int) bool {
		if all[i].Bps != all[j].Bps {
			return all[i].Bps > all[j].Bps
		}
		return all[i].IP.String() < all[j].IP.String()
	})
	if len(all) > n {
		all = all[:n]
	}
	if all == nil {
		return []TopEntry{}
	}
	return all
}
