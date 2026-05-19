package aggregate

// OverviewResult is the global aggregate view: a sum of per-host buckets across
// the entire active window, plus the count of hosts that have been seen in it.
//
// Buckets are newest-first (index 0 = `now`, index 59 = 59s ago).
type OverviewResult struct {
	Buckets     []Bucket
	ActiveHosts int
}

// Overview returns the global per-second buckets summed across every host
// active within the current WindowSize, plus the count of those hosts.
//
// Used by the dashboard to render the "what is the entire network doing"
// timeseries without pulling every host's snapshot client-side.
func (s *Store) Overview(now int64) OverviewResult {
	out := make([]Bucket, WindowSize)
	active := 0
	for _, sh := range s.shards {
		sh.mu.Lock()
		for _, hr := range sh.hosts {
			if now-hr.LastSec > WindowSize {
				continue
			}
			active++
			for i := 0; i < WindowSize; i++ {
				idx := int(((now-int64(i))%WindowSize+WindowSize)%WindowSize)
				out[i].Add(hr.Buckets[idx])
			}
		}
		sh.mu.Unlock()
	}
	return OverviewResult{Buckets: out, ActiveHosts: active}
}
