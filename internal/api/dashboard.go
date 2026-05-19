package api

import (
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/mitigador/mitigador/internal/aggregate"
	"github.com/mitigador/mitigador/internal/flow"
	"github.com/mitigador/mitigador/internal/ingest"
)

// Default page sizes for dashboard endpoints.
const (
	dashboardTopN       = 10
	dashboardRecentN    = 50
	dashboardRecentMaxN = 200
)

type dashOverviewBucket struct {
	Pps      uint64 `json:"pps"`
	Bps      uint64 `json:"bps"`
	PpsUDP   uint64 `json:"pps_udp"`
	BpsUDP   uint64 `json:"bps_udp"`
	PpsICMP  uint64 `json:"pps_icmp"`
	BpsICMP  uint64 `json:"bps_icmp"`
	PpsOther uint64 `json:"pps_other"`
	BpsOther uint64 `json:"bps_other"`
}

type dashTopEntry struct {
	IP            string   `json:"ip"`
	Hostname      string   `json:"hostname"` // reverse-DNS (PTR) — empty until cache populates
	Owner         string   `json:"owner"`    // ASN holder (Cloudflare, Google, …) from netowner table; empty if unknown
	Hostgroup     *string  `json:"hostgroup"`
	Bps           uint64   `json:"bps"`
	Pps           uint64   `json:"pps"`
	DominantProto string   `json:"dominant_proto"`
	Sparkline     []uint64 `json:"sparkline"` // bps per second, newest-first, len = WindowSize
}

type dashProtoBreakdown struct {
	UDP   uint64 `json:"udp"`
	ICMP  uint64 `json:"icmp"`
	Other uint64 `json:"other"`
}

type dashKPIs struct {
	BpsNow            uint64 `json:"bps_now"`              // bucket[0].Bps
	PpsNow            uint64 `json:"pps_now"`              // bucket[0].Pps
	BpsAvg            uint64 `json:"bps_avg"`              // mean over window
	PpsAvg            uint64 `json:"pps_avg"`              // mean over window
	ActiveHosts       int    `json:"active_hosts"`
	ExportersOnline   int    `json:"exporters_online"`
	ExportersTotal    int    `json:"exporters_total"`
}

type dashOverviewResponse struct {
	GeneratedAt   time.Time            `json:"generated_at"`
	KPIs          dashKPIs             `json:"kpis"`
	Buckets       []dashOverviewBucket `json:"buckets"`   // newest-first, len = 60
	ProtoBpsTotal dashProtoBreakdown   `json:"proto_bps"` // total bps share UDP/ICMP/Other over window
	ProtoPpsTotal dashProtoBreakdown   `json:"proto_pps"`
	Top           []dashTopEntry       `json:"top"`
}

// handleDashboardOverview handles GET /api/dashboard/overview.
// Aggregates the entire active window into one response so the dashboard makes
// a single 1Hz call instead of N per-host polls.
func handleDashboardOverview(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Store == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		now := time.Now()
		nowSec := now.Unix()

		ov := deps.Store.Overview(nowSec)
		buckets := make([]dashOverviewBucket, len(ov.Buckets))
		var totalBps, totalPps uint64
		var udpBps, icmpBps, otherBps uint64
		var udpPps, icmpPps, otherPps uint64
		for i, b := range ov.Buckets {
			buckets[i] = dashOverviewBucket{
				Pps:      b.Pps,
				Bps:      b.Bps,
				PpsUDP:   b.PpsUDP,
				BpsUDP:   b.BpsUDP,
				PpsICMP:  b.PpsICMP,
				BpsICMP:  b.BpsICMP,
				PpsOther: b.PpsOther,
				BpsOther: b.BpsOther,
			}
			totalBps += b.Bps
			totalPps += b.Pps
			udpBps += b.BpsUDP
			icmpBps += b.BpsICMP
			otherBps += b.BpsOther
			udpPps += b.PpsUDP
			icmpPps += b.PpsICMP
			otherPps += b.PpsOther
		}

		kpis := dashKPIs{
			ActiveHosts: ov.ActiveHosts,
		}
		if len(buckets) > 0 {
			kpis.BpsNow = buckets[0].Bps
			kpis.PpsNow = buckets[0].Pps
			kpis.BpsAvg = totalBps / uint64(len(buckets))
			kpis.PpsAvg = totalPps / uint64(len(buckets))
		}
		if deps.Inventory != nil && deps.Health != nil {
			snap := deps.Health.Snapshot(deps.Inventory, now)
			kpis.ExportersTotal = len(snap)
			for _, e := range snap {
				if e.Status == ingest.StatusOnline {
					kpis.ExportersOnline++
				}
			}
		}

		topEntries := deps.Store.Top(nowSec, dashboardTopN)
		topRows := make([]dashTopEntry, len(topEntries))
		for i, e := range topEntries {
			spark := make([]uint64, aggregate.WindowSize)
			snap := deps.Store.Snapshot(e.IP, nowSec, aggregate.WindowSize)
			for j, b := range snap {
				spark[j] = b.Bps
			}
			topRows[i] = dashTopEntry{
				IP:            e.IP.String(),
				Hostname:      resolveHostname(deps, e.IP),
				Owner:         resolveOwner(deps, e.IP),
				Hostgroup:     resolveHostgroup(deps, e.IP),
				Bps:           e.Bps,
				Pps:           e.Pps,
				DominantProto: e.DominantProto,
				Sparkline:     spark,
			}
		}

		writeJSON(w, http.StatusOK, dashOverviewResponse{
			GeneratedAt: now.UTC(),
			KPIs:        kpis,
			Buckets:     buckets,
			ProtoBpsTotal: dashProtoBreakdown{
				UDP:   udpBps,
				ICMP:  icmpBps,
				Other: otherBps,
			},
			ProtoPpsTotal: dashProtoBreakdown{
				UDP:   udpPps,
				ICMP:  icmpPps,
				Other: otherPps,
			},
			Top: topRows,
		})
	}
}

type dashRecentFlow struct {
	ReceivedMs   int64  `json:"received_ms"`
	SrcIP        string `json:"src_ip"`
	SrcHostname  string `json:"src_hostname"` // PTR, empty until cached
	SrcOwner     string `json:"src_owner"`    // ASN holder from netowner table; empty if unknown
	DstIP        string `json:"dst_ip"`
	DstHostname  string `json:"dst_hostname"`
	DstOwner     string `json:"dst_owner"`
	DstHostgroup string `json:"dst_hostgroup"` // longest-prefix-match hostgroup name; empty if none
	Proto        string `json:"proto"`
	Bytes        uint64 `json:"bytes"`
	Packets      uint64 `json:"packets"`
	AvgPktBytes  uint64 `json:"avg_pkt_bytes"` // bytes / packets (0 if packets == 0)
	SampleRate   uint32 `json:"sample_rate"`
	Exporter     string `json:"exporter"`
}

type dashRecentResponse struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Flows       []dashRecentFlow `json:"flows"`
}

// handleDashboardRecent handles GET /api/dashboard/recent[?n=N].
// Returns the latest flow.Records from the in-memory ring buffer, newest-first.
// n is clamped to [1, dashboardRecentMaxN]; default = dashboardRecentN.
func handleDashboardRecent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.RecentFlows == nil {
			writeJSON(w, http.StatusOK, dashRecentResponse{
				GeneratedAt: time.Now().UTC(),
				Flows:       []dashRecentFlow{},
			})
			return
		}
		n := dashboardRecentN
		if s := r.URL.Query().Get("n"); s != "" {
			if v, err := strconv.Atoi(s); err == nil {
				n = v
			}
		}
		if n < 1 {
			n = 1
		}
		if n > dashboardRecentMaxN {
			n = dashboardRecentMaxN
		}
		snap := deps.RecentFlows.Snapshot(n)
		out := make([]dashRecentFlow, len(snap))
		for i, rec := range snap {
			var avg uint64
			if rec.Packets > 0 {
				avg = rec.Bytes / rec.Packets
			}
			hg := ""
			if h := resolveHostgroup(deps, rec.DstIP); h != nil {
				hg = *h
			}
			out[i] = dashRecentFlow{
				ReceivedMs:   rec.Received.UnixMilli(),
				SrcIP:        rec.SrcIP.String(),
				SrcHostname:  resolveHostname(deps, rec.SrcIP),
				SrcOwner:     resolveOwner(deps, rec.SrcIP),
				DstIP:        rec.DstIP.String(),
				DstHostname:  resolveHostname(deps, rec.DstIP),
				DstOwner:     resolveOwner(deps, rec.DstIP),
				DstHostgroup: hg,
				Proto:        protoLabel(rec.Proto),
				Bytes:        rec.Bytes,
				Packets:      rec.Packets,
				AvgPktBytes:  avg,
				SampleRate:   rec.SampleRate,
				Exporter:     rec.Exporter.String(),
			}
		}
		writeJSON(w, http.StatusOK, dashRecentResponse{
			GeneratedAt: time.Now().UTC(),
			Flows:       out,
		})
	}
}

func resolveHostname(deps Deps, ip netip.Addr) string {
	if deps.DNS == nil {
		return ""
	}
	return deps.DNS.Lookup(ip)
}

func resolveOwner(deps Deps, ip netip.Addr) string {
	if deps.NetOwner == nil {
		return ""
	}
	return deps.NetOwner.Lookup(ip)
}

func protoLabel(p flow.Proto) string {
	switch p {
	case flow.ProtoUDP:
		return "udp"
	case flow.ProtoTCP:
		return "tcp"
	case flow.ProtoICMP, flow.ProtoICMPv6:
		return "icmp"
	default:
		return "other"
	}
}
