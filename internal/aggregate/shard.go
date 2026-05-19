package aggregate

import (
	"net/netip"
	"sync"
)

// Shard is one slice of the sharded map.
type Shard struct {
	mu    sync.Mutex
	hosts map[netip.Addr]*HostRing
}

func newShard() *Shard {
	return &Shard{hosts: make(map[netip.Addr]*HostRing)}
}
