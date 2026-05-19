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
	CountryISO    string   `json:"country_iso"`
	CountryName   string   `json:"country_name"`
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
			iso, cname := resolveCountry(deps, e.IP)
			topRows[i] = dashTopEntry{
				IP:            e.IP.String(),
				Hostname:      resolveHostname(deps, e.IP),
				Owner:         resolveOwner(deps, e.IP),
				CountryISO:    iso,
				CountryName:   cname,
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

// ─── Analytics endpoint (Akvorado-flavored aggregations) ──────────────

type dashOwnerBucket struct {
	Owner string `json:"owner"`
	Bps   uint64 `json:"bps"`
	Pps   uint64 `json:"pps"`
	Hosts int    `json:"hosts"` // distinct destination IPs in this owner
}

type dashCountryBucket struct {
	ISO   string `json:"iso"`   // ISO-3166 alpha-2
	Name  string `json:"name"`  // English country name
	Bps   uint64 `json:"bps"`
	Pps   uint64 `json:"pps"`
	Hosts int    `json:"hosts"`
}

type dashSankeyEdge struct {
	Source string `json:"source"` // src owner (or "private/127.x.x.x" for unmapped)
	Target string `json:"target"` // dst owner / hostgroup / country
	Bytes  uint64 `json:"bytes"`
	Count  int    `json:"count"`
}

type dashAnalyticsResponse struct {
	GeneratedAt  time.Time           `json:"generated_at"`
	TopOwners    []dashOwnerBucket   `json:"top_owners"`
	TopCountries []dashCountryBucket `json:"top_countries"`
	Sankey       []dashSankeyEdge    `json:"sankey"`
}

// handleDashboardAnalytics handles GET /api/dashboard/analytics.
// Returns three Akvorado-style aggregations in one shot so the UI makes a
// single 1Hz poll for all of them:
//   - top_owners:     bps/pps summed across destinations by AS organization
//   - top_countries:  bps/pps summed across destinations by ISO-3166 country
//   - sankey:         src_owner → dst_owner edges built from the recent-flows
//                     ring buffer (last ~500 flow records)
func handleDashboardAnalytics(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Store == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		now := time.Now()
		nowSec := now.Unix()

		// Pull every active destination — Top(now, n) with a large n gives us
		// the universe of currently-tracked hosts. 1024 is the cap; an ISP
		// with more than that in a 60s window has bigger problems anyway.
		entries := deps.Store.Top(nowSec, 1024)

		type ownerAgg struct {
			bps   uint64
			pps   uint64
			hosts int
		}
		type countryAgg struct {
			name  string
			bps   uint64
			pps   uint64
			hosts int
		}
		owners := map[string]*ownerAgg{}
		countries := map[string]*countryAgg{}

		for _, e := range entries {
			owner := resolveOwner(deps, e.IP)
			if owner == "" {
				owner = "—"
			}
			oa, ok := owners[owner]
			if !ok {
				oa = &ownerAgg{}
				owners[owner] = oa
			}
			oa.bps += e.Bps
			oa.pps += e.Pps
			oa.hosts++

			iso, cname := resolveCountry(deps, e.IP)
			if iso == "" {
				iso = "??"
				cname = "Unknown"
			}
			ca, ok := countries[iso]
			if !ok {
				ca = &countryAgg{name: cname}
				countries[iso] = ca
			}
			ca.bps += e.Bps
			ca.pps += e.Pps
			ca.hosts++
		}

		topOwners := make([]dashOwnerBucket, 0, len(owners))
		for name, a := range owners {
			topOwners = append(topOwners, dashOwnerBucket{Owner: name, Bps: a.bps, Pps: a.pps, Hosts: a.hosts})
		}
		sortByBps(topOwners, func(b dashOwnerBucket) uint64 { return b.Bps })
		if len(topOwners) > 12 {
			topOwners = topOwners[:12]
		}

		topCountries := make([]dashCountryBucket, 0, len(countries))
		for iso, a := range countries {
			topCountries = append(topCountries, dashCountryBucket{ISO: iso, Name: a.name, Bps: a.bps, Pps: a.pps, Hosts: a.hosts})
		}
		sortByBps(topCountries, func(b dashCountryBucket) uint64 { return b.Bps })
		if len(topCountries) > 12 {
			topCountries = topCountries[:12]
		}

		// Sankey edges from the recent-flows ring buffer.
		//
		// We need a bipartite graph: ECharts sankey rejects cycles, and ISP
		// traffic naturally produces them (Google→Vivo inbound, Vivo→Google
		// outbound). The fix is to namespace every source node under "◂ " and
		// every target under " ▸" so the same organization name becomes two
		// distinct nodes — one on the left column, one on the right.
		type edgeKey struct{ src, dst string }
		edges := map[edgeKey]*dashSankeyEdge{}
		labelize := func(ip netip.Addr) string {
			if name := resolveOwner(deps, ip); name != "" {
				return name
			}
			if ip.IsPrivate() || ip.IsLoopback() {
				return "private"
			}
			if iso, _ := resolveCountry(deps, ip); iso != "" {
				return "country:" + iso
			}
			return "unknown"
		}
		if deps.RecentFlows != nil {
			snap := deps.RecentFlows.Snapshot(500)
			for _, rec := range snap {
				src := "◂ " + labelize(rec.SrcIP)
				dst := labelize(rec.DstIP) + " ▸"
				key := edgeKey{src, dst}
				e, ok := edges[key]
				if !ok {
					e = &dashSankeyEdge{Source: src, Target: dst}
					edges[key] = e
				}
				e.Bytes += rec.Bytes
				e.Count++
			}
		}
		sankey := make([]dashSankeyEdge, 0, len(edges))
		for _, e := range edges {
			sankey = append(sankey, *e)
		}
		sortByBytes(sankey)
		if len(sankey) > 40 {
			sankey = sankey[:40]
		}

		writeJSON(w, http.StatusOK, dashAnalyticsResponse{
			GeneratedAt:  now.UTC(),
			TopOwners:    topOwners,
			TopCountries: topCountries,
			Sankey:       sankey,
		})
	}
}

// sortByBps sorts a slice of buckets by their Bps field, descending.
// Uses a closure to extract the value so a single helper works for both
// owner and country buckets without reflection.
func sortByBps[T any](buckets []T, bps func(T) uint64) {
	// Simple insertion sort — N <= 1024 in practice (Top cap).
	for i := 1; i < len(buckets); i++ {
		for j := i; j > 0 && bps(buckets[j]) > bps(buckets[j-1]); j-- {
			buckets[j], buckets[j-1] = buckets[j-1], buckets[j]
		}
	}
}

func sortByBytes(edges []dashSankeyEdge) {
	for i := 1; i < len(edges); i++ {
		for j := i; j > 0 && edges[j].Bytes > edges[j-1].Bytes; j-- {
			edges[j], edges[j-1] = edges[j-1], edges[j]
		}
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

func resolveCountry(deps Deps, ip netip.Addr) (iso, name string) {
	if deps.NetOwner == nil {
		return "", ""
	}
	return deps.NetOwner.CountryISO(ip), deps.NetOwner.CountryName(ip)
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
