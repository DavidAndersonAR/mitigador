// Package dns provides bounded, cached reverse-DNS resolution for the dashboard.
//
// rDNS calls are non-blocking from the handler's point of view: the cache is
// consulted synchronously, and a cache miss kicks off an async lookup that
// fills the entry for the next poll. This keeps per-request latency flat
// regardless of upstream DNS performance.
package dns

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	hitTTL     = 5 * time.Minute
	missTTL    = 30 * time.Second
	maxEntries = 5000
	lookupTO   = 2 * time.Second
)

type entry struct {
	name      string
	expiresAt time.Time
}

// Resolver is a thread-safe PTR cache with async lookups.
type Resolver struct {
	mu       sync.RWMutex
	cache    map[netip.Addr]entry
	pending  map[netip.Addr]struct{}
	resolver *net.Resolver
}

// NewResolver builds a resolver that uses Go's default DNS configuration.
func NewResolver() *Resolver {
	return &Resolver{
		cache:    make(map[netip.Addr]entry, 256),
		pending:  make(map[netip.Addr]struct{}, 32),
		resolver: net.DefaultResolver,
	}
}

// Lookup returns the cached PTR name for ip (without the trailing dot) or
// the empty string if no name has been resolved yet. A cache miss schedules
// an async lookup; the next call will have the answer.
//
// Special-case: loopback and private/link-local IPs short-circuit to empty —
// no point in spamming the system resolver for them.
func (r *Resolver) Lookup(ip netip.Addr) string {
	if !ip.IsValid() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return ""
	}
	now := time.Now()
	r.mu.RLock()
	if e, ok := r.cache[ip]; ok && now.Before(e.expiresAt) {
		r.mu.RUnlock()
		return e.name
	}
	r.mu.RUnlock()

	r.mu.Lock()
	if _, busy := r.pending[ip]; busy {
		r.mu.Unlock()
		return ""
	}
	if len(r.cache) >= maxEntries {
		// Evict expired or arbitrary entries — keep cache size bounded.
		for k, v := range r.cache {
			if now.After(v.expiresAt) {
				delete(r.cache, k)
			}
			if len(r.cache) < maxEntries {
				break
			}
		}
		// If still full, drop any one entry to make room.
		if len(r.cache) >= maxEntries {
			for k := range r.cache {
				delete(r.cache, k)
				break
			}
		}
	}
	r.pending[ip] = struct{}{}
	r.mu.Unlock()

	go r.fetch(ip)
	return ""
}

func (r *Resolver) fetch(ip netip.Addr) {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTO)
	defer cancel()
	names, _ := r.resolver.LookupAddr(ctx, ip.String())
	name := ""
	if len(names) > 0 {
		name = strings.TrimSuffix(names[0], ".")
	}
	ttl := hitTTL
	if name == "" {
		ttl = missTTL
	}
	r.mu.Lock()
	r.cache[ip] = entry{name: name, expiresAt: time.Now().Add(ttl)}
	delete(r.pending, ip)
	r.mu.Unlock()
}

// Stats returns the current cache size and pending lookup count.
// Intended for /metrics or debug pages.
func (r *Resolver) Stats() (cached, pending int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache), len(r.pending)
}
