// Package subscriber resolves an IP address (typically a CGN-internal
// address like 100.64.x.y) to the human-identifiable subscriber that
// currently owns the address — PPPoE username, DHCP lease, or static
// mapping.
//
// The Store is the single in-memory authority. Writers (the Mikrotik
// poller, static-config loader) replace the map snapshot atomically;
// readers (the dashboard handlers) take an RLock and look up by IP.
// This keeps the lookup path lock-free under contention.
package subscriber

import (
	"net/netip"
	"sync"
	"time"
)

// Subscriber is one resolved identity.
type Subscriber struct {
	// IP currently assigned to this subscriber. Acts as the map key.
	IP netip.Addr

	// Username is the operator-meaningful identity (e.g. PPPoE "joao.silva",
	// DHCP host-name, or static "Cliente Empresa XYZ").
	Username string

	// Service is the channel that resolved this subscriber:
	//   "pppoe" | "dhcp" | "hotspot" | "static"
	Service string

	// Router is the friendly name of the source router (e.g. "BR1"), so a
	// multi-MK setup can disambiguate identical usernames per device.
	Router string

	// Comment carries operator notes (caller-id MAC, plan name, etc.).
	Comment string

	// ConnectedSince records when the session started (best-effort —
	// may be zero for sources that do not surface uptime).
	ConnectedSince time.Time

	// LastSeen is when the poller last confirmed this subscriber was active.
	// Used to evict stale entries when a router goes offline.
	LastSeen time.Time
}

// Store is the lockless-read in-memory subscriber index.
type Store struct {
	mu sync.RWMutex
	m  map[netip.Addr]*Subscriber
}

// New returns an empty store.
func New() *Store {
	return &Store{m: make(map[netip.Addr]*Subscriber)}
}

// Lookup returns the subscriber currently owning ip, or (nil, false).
// IPv4-mapped IPv6 addresses are normalized so the lookup hits regardless
// of how the caller obtained the address.
func (s *Store) Lookup(ip netip.Addr) (*Subscriber, bool) {
	ip = ip.Unmap()
	s.mu.RLock()
	defer s.mu.RUnlock()
	sub, ok := s.m[ip]
	return sub, ok
}

// Replace atomically swaps the entire map for the given snapshot.
// Callers (the poller) prepare the new map outside the lock and then call
// this once per refresh cycle — avoids the readers seeing partial state.
func (s *Store) Replace(next map[netip.Addr]*Subscriber) {
	for k, v := range next {
		if k != k.Unmap() {
			delete(next, k)
			v.IP = k.Unmap()
			next[v.IP] = v
		}
	}
	s.mu.Lock()
	s.m = next
	s.mu.Unlock()
}

// Len returns the current number of resolved subscribers.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

// All returns a snapshot slice of every subscriber (useful for /api/subscribers
// in the future and for debug pages).
func (s *Store) All() []Subscriber {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Subscriber, 0, len(s.m))
	for _, sub := range s.m {
		out = append(out, *sub)
	}
	return out
}
