package detect_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/aggregate"
	"github.com/mitigador/mitigador/internal/detect"
	"github.com/mitigador/mitigador/internal/flow"
)

// buildCat is a helper for engine tests.
func buildCat(thresholds ...detect.Threshold) *detect.Catalog {
	return detect.NewCatalogFromThresholds(thresholds)
}

func TestEngine_DetectsUDPFlood_EndToEnd(t *testing.T) {
	store := aggregate.New(2)
	cat := buildCat(detect.Threshold{
		HostgroupName: "corp",
		Prefix:        netip.MustParsePrefix("192.0.2.0/24"),
		Vector:        detect.VectorUDPFlood,
		PPS:           10, BPS: 100,
		MinWindowSec: 2, GraceSec: 3,
	})
	out := make(chan detect.AttackEvent, 10)
	e := detect.NewEngine(store, cat, out)

	ip := netip.MustParseAddr("192.0.2.5")
	now := int64(1000)

	// Inject violating UDP traffic for 5 seconds.
	for sec := now; sec < now+5; sec++ {
		for i := 0; i < 100; i++ {
			store.Update(ip, sec, flow.Record{
				Proto:   flow.ProtoUDP,
				Packets: 1,
				Bytes:   10,
			})
		}
	}

	// Drive 4 ticks manually — needs DurationViolated >= MinWindowSec=2.
	for sec := now; sec < now+4; sec++ {
		e.Tick(time.Unix(sec, 0))
	}

	select {
	case ev := <-out:
		if ev.State != detect.StateStarted {
			t.Errorf("expected StateStarted, got %s", ev.State)
		}
		if ev.Vector != detect.VectorUDPFlood {
			t.Errorf("expected VectorUDPFlood, got %s", ev.Vector)
		}
		if ev.HostIP != ip {
			t.Errorf("expected HostIP=%s, got %s", ip, ev.HostIP)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected StateStarted event within timeout")
	}
}

func TestEngine_NoThreshold_NoDetection(t *testing.T) {
	store := aggregate.New(2)
	// Only "corp" prefix is configured — 8.8.8.8 is not in any hostgroup.
	cat := buildCat(detect.Threshold{
		HostgroupName: "corp",
		Prefix:        netip.MustParsePrefix("192.0.2.0/24"),
		Vector:        detect.VectorUDPFlood,
		PPS:           10, BPS: 100,
		MinWindowSec: 2, GraceSec: 3,
	})
	out := make(chan detect.AttackEvent, 10)
	e := detect.NewEngine(store, cat, out)

	ip := netip.MustParseAddr("8.8.8.8")
	now := int64(2000)

	// Inject large UDP traffic to unregistered host.
	for sec := now; sec < now+10; sec++ {
		for i := 0; i < 10_000; i++ {
			store.Update(ip, sec, flow.Record{
				Proto:   flow.ProtoUDP,
				Packets: 1,
				Bytes:   10,
			})
		}
	}

	// Drive several ticks.
	for sec := now; sec < now+10; sec++ {
		e.Tick(time.Unix(sec, 0))
	}

	// No event should be emitted (DETE-01: no detection without configured threshold).
	select {
	case ev := <-out:
		t.Errorf("expected no event for unregistered host, got %s for %s", ev.State, ev.HostIP)
	default:
		// Good — no events.
	}
}

func TestEngine_ICMPFlood_DetectedByVector(t *testing.T) {
	store := aggregate.New(2)
	cat := buildCat(detect.Threshold{
		HostgroupName: "corp",
		Prefix:        netip.MustParsePrefix("192.0.2.0/24"),
		Vector:        detect.VectorICMPFlood,
		PPS:           10, BPS: 100,
		MinWindowSec: 2, GraceSec: 3,
	})
	out := make(chan detect.AttackEvent, 10)
	e := detect.NewEngine(store, cat, out)

	ip := netip.MustParseAddr("192.0.2.10")
	now := int64(3000)

	// Inject ICMP-dominant traffic.
	for sec := now; sec < now+5; sec++ {
		for i := 0; i < 100; i++ {
			store.Update(ip, sec, flow.Record{
				Proto:   flow.ProtoICMP,
				Packets: 1,
				Bytes:   10,
			})
		}
	}

	for sec := now; sec < now+4; sec++ {
		e.Tick(time.Unix(sec, 0))
	}

	select {
	case ev := <-out:
		if ev.Vector != detect.VectorICMPFlood {
			t.Errorf("expected VectorICMPFlood, got %s", ev.Vector)
		}
		if ev.State != detect.StateStarted {
			t.Errorf("expected StateStarted, got %s", ev.State)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected StateStarted for ICMP flood")
	}
}

func TestEngine_DroppedCounter_IncrementsOnFullChannel(t *testing.T) {
	store := aggregate.New(2)
	cat := buildCat(detect.Threshold{
		HostgroupName: "corp",
		Prefix:        netip.MustParsePrefix("192.0.2.0/24"),
		Vector:        detect.VectorUDPFlood,
		PPS:           10, BPS: 100,
		MinWindowSec: 2, GraceSec: 3,
	})
	// Zero-capacity channel — every send will be dropped.
	out := make(chan detect.AttackEvent, 0)
	e := detect.NewEngine(store, cat, out)

	ip := netip.MustParseAddr("192.0.2.20")
	now := int64(4000)

	for sec := now; sec < now+5; sec++ {
		for i := 0; i < 100; i++ {
			store.Update(ip, sec, flow.Record{
				Proto:   flow.ProtoUDP,
				Packets: 1,
				Bytes:   10,
			})
		}
	}

	for sec := now; sec < now+4; sec++ {
		e.Tick(time.Unix(sec, 0))
	}

	// After a STARTED would have fired, the Dropped counter should be >= 1.
	if e.Dropped() == 0 {
		t.Error("expected Dropped() > 0 when out channel is zero-capacity")
	}
}
