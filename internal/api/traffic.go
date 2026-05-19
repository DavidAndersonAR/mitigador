package api

import (
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mitigador/mitigador/internal/aggregate"
)

// maxIPLen guards against absurd path-param input before parsing.
// Longest textual IPv6 (with embedded IPv4) is 45 chars; we cap at 64 for safety.
const maxIPLen = 64

type topTalkerItem struct {
	IP            string  `json:"ip"`
	Hostgroup     *string `json:"hostgroup"`
	Bps           uint64  `json:"bps"`
	Pps           uint64  `json:"pps"`
	DominantProto string  `json:"dominant_proto"`
}

type top20Payload struct {
	Items       []topTalkerItem `json:"items"`
	GeneratedAt time.Time       `json:"generated_at"`
}

type hostBucketItem struct {
	OffsetS  int    `json:"offset_s"`
	Pps      uint64 `json:"pps"`
	Bps      uint64 `json:"bps"`
	PpsUDP   uint64 `json:"pps_udp"`
	BpsUDP   uint64 `json:"bps_udp"`
	PpsICMP  uint64 `json:"pps_icmp"`
	BpsICMP  uint64 `json:"bps_icmp"`
	PpsOther uint64 `json:"pps_other"`
	BpsOther uint64 `json:"bps_other"`
}

type hostPayload struct {
	IP          string           `json:"ip"`
	Hostgroup   *string          `json:"hostgroup"`
	GeneratedAt time.Time        `json:"generated_at"`
	Buckets     []hostBucketItem `json:"buckets"`
}

// resolveHostgroup returns the longest-prefix-match hostgroup name (or nil).
// Defensive: returns nil if Catalog is nil (avoids panic in tests that don't wire it).
func resolveHostgroup(deps Deps, ip netip.Addr) *string {
	if deps.Catalog == nil {
		return nil
	}
	matches := deps.Catalog.Lookup(ip)
	if len(matches) == 0 {
		return nil
	}
	name := matches[0].HostgroupName
	return &name
}

// handleTrafficTop20 handles GET /api/traffic/top20.
// Returns the top 20 hosts by bps over the current 60s window (D-03, D-04, D-05).
// Always 200, never 404 — empty store yields {"items":[], "generated_at":…}.
func handleTrafficTop20(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Store == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		now := time.Now()
		entries := deps.Store.Top(now.Unix(), 20)
		items := make([]topTalkerItem, len(entries))
		for i, e := range entries {
			items[i] = topTalkerItem{
				IP:            e.IP.String(),
				Hostgroup:     resolveHostgroup(deps, e.IP),
				Bps:           e.Bps,
				Pps:           e.Pps,
				DominantProto: e.DominantProto,
			}
		}
		writeJSON(w, http.StatusOK, top20Payload{Items: items, GeneratedAt: now.UTC()})
	}
}

// handleTrafficHost handles GET /api/traffic/host/{ip}.
// Validation:
//   - {ip} must be <= maxIPLen chars (T-01.1-02: log-injection / DoS mitigation)
//   - {ip} must parse via netip.ParseAddr; otherwise 400 invalid_param
//
// Returns 404 with no detail when the host is not in the active window — protects
// against IP enumeration (T-01.1-03).
func handleTrafficHost(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Store == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		ipStr := chi.URLParam(r, "ip")
		if len(ipStr) == 0 || len(ipStr) > maxIPLen {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param", "param": "ip"})
			return
		}
		addr, err := netip.ParseAddr(ipStr)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param", "param": "ip"})
			return
		}
		now := time.Now()
		buckets := deps.Store.Snapshot(addr, now.Unix(), aggregate.WindowSize)
		if buckets == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		items := make([]hostBucketItem, len(buckets))
		for i, b := range buckets {
			items[i] = hostBucketItem{
				OffsetS:  i,
				Pps:      b.Pps,
				Bps:      b.Bps,
				PpsUDP:   b.PpsUDP,
				BpsUDP:   b.BpsUDP,
				PpsICMP:  b.PpsICMP,
				BpsICMP:  b.BpsICMP,
				PpsOther: b.PpsOther,
				BpsOther: b.BpsOther,
			}
		}
		writeJSON(w, http.StatusOK, hostPayload{
			IP:          addr.String(),
			Hostgroup:   resolveHostgroup(deps, addr),
			GeneratedAt: now.UTC(),
			Buckets:     items,
		})
	}
}
