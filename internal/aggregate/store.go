package aggregate

import (
	"hash/fnv"
	"net/netip"
	"runtime"

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
