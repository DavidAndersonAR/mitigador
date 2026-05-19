package detect_test

import (
	"net/netip"
	"testing"

	"github.com/mitigador/mitigador/internal/detect"
)

func TestCatalog_LongestPrefixMatch(t *testing.T) {
	thresholds := []detect.Threshold{
		{
			HostgroupName: "infra",
			Prefix:        netip.MustParsePrefix("10.0.0.0/8"),
			Vector:        detect.VectorUDPFlood,
			PPS:           1000, BPS: 100_000,
			MinWindowSec: 5, GraceSec: 60,
		},
		{
			HostgroupName: "corp",
			Prefix:        netip.MustParsePrefix("192.0.2.0/24"),
			Vector:        detect.VectorUDPFlood,
			PPS:           500, BPS: 50_000,
			MinWindowSec: 5, GraceSec: 60,
		},
	}
	cat := detect.NewCatalogFromThresholds(thresholds)

	// 192.0.2.5 matches "corp" (/24) not "infra" (/8) — longest prefix wins.
	result := cat.Lookup(netip.MustParseAddr("192.0.2.5"))
	if len(result) != 1 {
		t.Fatalf("expected 1 threshold, got %d", len(result))
	}
	if result[0].HostgroupName != "corp" {
		t.Errorf("expected hostgroup=corp, got %s", result[0].HostgroupName)
	}
}

func TestCatalog_NoMatch_ReturnsEmpty(t *testing.T) {
	thresholds := []detect.Threshold{
		{
			HostgroupName: "infra",
			Prefix:        netip.MustParsePrefix("10.0.0.0/8"),
			Vector:        detect.VectorUDPFlood,
			PPS:           1000, BPS: 100_000,
			MinWindowSec: 5, GraceSec: 60,
		},
	}
	cat := detect.NewCatalogFromThresholds(thresholds)

	result := cat.Lookup(netip.MustParseAddr("8.8.8.8"))
	if len(result) != 0 {
		t.Errorf("expected empty result for unmatched IP, got %d thresholds", len(result))
	}
}

func TestCatalog_MultipleVectorsSameHostgroup(t *testing.T) {
	thresholds := []detect.Threshold{
		{
			HostgroupName: "corp",
			Prefix:        netip.MustParsePrefix("192.0.2.0/24"),
			Vector:        detect.VectorUDPFlood,
			PPS:           500, BPS: 50_000,
			MinWindowSec: 5, GraceSec: 60,
		},
		{
			HostgroupName: "corp",
			Prefix:        netip.MustParsePrefix("192.0.2.0/24"),
			Vector:        detect.VectorICMPFlood,
			PPS:           200, BPS: 20_000,
			MinWindowSec: 3, GraceSec: 30,
		},
	}
	cat := detect.NewCatalogFromThresholds(thresholds)

	result := cat.Lookup(netip.MustParseAddr("192.0.2.5"))
	if len(result) != 2 {
		t.Fatalf("expected 2 thresholds (one per vector), got %d", len(result))
	}
}

func TestCatalog_LookupByVector(t *testing.T) {
	thresholds := []detect.Threshold{
		{
			HostgroupName: "corp",
			Prefix:        netip.MustParsePrefix("192.0.2.0/24"),
			Vector:        detect.VectorUDPFlood,
			PPS:           500, BPS: 50_000,
			MinWindowSec: 5, GraceSec: 60,
		},
		{
			HostgroupName: "corp",
			Prefix:        netip.MustParsePrefix("192.0.2.0/24"),
			Vector:        detect.VectorICMPFlood,
			PPS:           200, BPS: 20_000,
			MinWindowSec: 3, GraceSec: 30,
		},
	}
	cat := detect.NewCatalogFromThresholds(thresholds)
	ip := netip.MustParseAddr("192.0.2.5")

	udp, ok := cat.LookupByVector(ip, detect.VectorUDPFlood)
	if !ok {
		t.Fatal("expected UDP threshold to be found")
	}
	if udp.Vector != detect.VectorUDPFlood {
		t.Errorf("expected VectorUDPFlood, got %s", udp.Vector)
	}
	if udp.PPS != 500 {
		t.Errorf("expected PPS=500, got %d", udp.PPS)
	}

	icmp, ok := cat.LookupByVector(ip, detect.VectorICMPFlood)
	if !ok {
		t.Fatal("expected ICMP threshold to be found")
	}
	if icmp.PPS != 200 {
		t.Errorf("expected PPS=200, got %d", icmp.PPS)
	}

	// No TCP vector configured.
	_, ok = cat.LookupByVector(ip, detect.Vector("tcp_flood"))
	if ok {
		t.Error("expected no threshold for tcp_flood")
	}
}
