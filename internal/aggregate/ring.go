package aggregate

// WindowSize is the number of 1-second buckets in a host's ring.
const WindowSize = 60

// HostRing is per-host state inside a shard.
type HostRing struct {
	Buckets [WindowSize]Bucket
	LastSec int64 // last second this host received traffic
}
