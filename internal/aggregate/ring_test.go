package aggregate_test

import (
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/aggregate"
	"github.com/mitigador/mitigador/internal/flow"
)

func TestStore_UpdateAndSnapshot_SingleHost(t *testing.T) {
	s := aggregate.New(4)
	ip := netip.MustParseAddr("192.0.2.1")
	s.Update(ip, 100, flow.Record{Proto: flow.ProtoUDP, Packets: 10, Bytes: 1000, Received: time.Unix(100, 0)})
	bs := s.Snapshot(ip, 100, 1)
	if len(bs) != 1 {
		t.Fatalf("len=%d", len(bs))
	}
	b := bs[0]
	if b.Pps != 10 || b.Bps != 1000 {
		t.Errorf("Pps=%d Bps=%d, want 10/1000", b.Pps, b.Bps)
	}
	if b.PpsUDP != 10 || b.BpsUDP != 1000 {
		t.Errorf("UDP: Pps=%d Bps=%d, want 10/1000", b.PpsUDP, b.BpsUDP)
	}
	if b.PpsICMP != 0 || b.PpsOther != 0 {
		t.Errorf("non-UDP nonzero: ICMP=%d Other=%d", b.PpsICMP, b.PpsOther)
	}
}

func TestStore_UpdateAccumulates(t *testing.T) {
	s := aggregate.New(4)
	ip := netip.MustParseAddr("192.0.2.2")
	s.Update(ip, 100, flow.Record{Proto: flow.ProtoUDP, Packets: 5, Bytes: 500})
	s.Update(ip, 100, flow.Record{Proto: flow.ProtoUDP, Packets: 3, Bytes: 300})
	bs := s.Snapshot(ip, 100, 1)
	if len(bs) != 1 {
		t.Fatalf("len=%d", len(bs))
	}
	b := bs[0]
	if b.Pps != 8 || b.Bps != 800 {
		t.Errorf("Pps=%d Bps=%d, want 8/800", b.Pps, b.Bps)
	}
	if b.PpsUDP != 8 || b.BpsUDP != 800 {
		t.Errorf("UDP: Pps=%d Bps=%d, want 8/800", b.PpsUDP, b.BpsUDP)
	}
}

func TestStore_UpdateAcrossSeconds(t *testing.T) {
	s := aggregate.New(4)
	ip := netip.MustParseAddr("192.0.2.3")
	s.Update(ip, 100, flow.Record{Proto: flow.ProtoTCP, Packets: 10, Bytes: 1000})
	s.Update(ip, 101, flow.Record{Proto: flow.ProtoTCP, Packets: 20, Bytes: 2000})
	bs := s.Snapshot(ip, 101, 2)
	if len(bs) != 2 {
		t.Fatalf("len=%d, want 2", len(bs))
	}
	// index 0 = newest (sec 101), index 1 = sec 100
	if bs[0].Pps != 20 || bs[0].Bps != 2000 {
		t.Errorf("bs[0] Pps=%d Bps=%d, want 20/2000", bs[0].Pps, bs[0].Bps)
	}
	if bs[1].Pps != 10 || bs[1].Bps != 1000 {
		t.Errorf("bs[1] Pps=%d Bps=%d, want 10/1000", bs[1].Pps, bs[1].Bps)
	}
}

func TestStore_SnapshotUnknownIP_ReturnsNil(t *testing.T) {
	s := aggregate.New(4)
	ip := netip.MustParseAddr("10.0.0.1")
	bs := s.Snapshot(ip, 100, 1)
	if bs != nil {
		t.Errorf("expected nil, got %v", bs)
	}
}

func TestStore_SnapshotWindowClampedToWindowSize(t *testing.T) {
	s := aggregate.New(4)
	ip := netip.MustParseAddr("192.0.2.4")
	s.Update(ip, 100, flow.Record{Proto: flow.ProtoUDP, Packets: 1, Bytes: 100})
	// Request more than WindowSize — should be clamped to WindowSize.
	bs := s.Snapshot(ip, 100, aggregate.WindowSize+100)
	if len(bs) != aggregate.WindowSize {
		t.Errorf("len=%d, want %d", len(bs), aggregate.WindowSize)
	}
}

func TestStore_Proto_BreakdownByVector(t *testing.T) {
	s := aggregate.New(4)
	ip := netip.MustParseAddr("192.0.2.5")
	s.Update(ip, 100, flow.Record{Proto: flow.ProtoUDP, Packets: 10, Bytes: 1000})
	s.Update(ip, 100, flow.Record{Proto: flow.ProtoICMP, Packets: 5, Bytes: 500})
	s.Update(ip, 100, flow.Record{Proto: flow.ProtoTCP, Packets: 3, Bytes: 300})
	bs := s.Snapshot(ip, 100, 1)
	if len(bs) != 1 {
		t.Fatalf("len=%d", len(bs))
	}
	b := bs[0]
	if b.Pps != 18 || b.Bps != 1800 {
		t.Errorf("total Pps=%d Bps=%d, want 18/1800", b.Pps, b.Bps)
	}
	if b.PpsUDP != 10 || b.BpsUDP != 1000 {
		t.Errorf("UDP Pps=%d Bps=%d, want 10/1000", b.PpsUDP, b.BpsUDP)
	}
	if b.PpsICMP != 5 || b.BpsICMP != 500 {
		t.Errorf("ICMP Pps=%d Bps=%d, want 5/500", b.PpsICMP, b.BpsICMP)
	}
	if b.PpsOther != 3 || b.BpsOther != 300 {
		t.Errorf("Other Pps=%d Bps=%d, want 3/300", b.PpsOther, b.BpsOther)
	}
}

func TestStore_ConcurrentUpdates_NoRace(t *testing.T) {
	s := aggregate.New(8)
	ip := netip.MustParseAddr("192.0.2.99")
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10000; i++ {
				s.Update(ip, 100, flow.Record{Proto: flow.ProtoUDP, Packets: 1, Bytes: 100})
			}
		}()
	}
	wg.Wait()
	bs := s.Snapshot(ip, 100, 1)
	if bs[0].Pps != 80000 || bs[0].Bps != 8_000_000 {
		t.Errorf("Pps=%d Bps=%d, want 80000/8000000", bs[0].Pps, bs[0].Bps)
	}
}

func TestStore_ShardForDeterministic(t *testing.T) {
	// Same IP must always map to same shard — verify Update is idempotent
	// across multiple Store instances with the same shard count.
	s1 := aggregate.New(8)
	s2 := aggregate.New(8)
	ip := netip.MustParseAddr("203.0.113.1")
	r := flow.Record{Proto: flow.ProtoUDP, Packets: 1, Bytes: 100}
	s1.Update(ip, 100, r)
	s2.Update(ip, 100, r)
	bs1 := s1.Snapshot(ip, 100, 1)
	bs2 := s2.Snapshot(ip, 100, 1)
	if bs1[0].Pps != bs2[0].Pps {
		t.Errorf("shard routing not deterministic: s1.Pps=%d s2.Pps=%d", bs1[0].Pps, bs2[0].Pps)
	}
}

func TestStore_DefaultShardCount_NonZero(t *testing.T) {
	// New(0) must produce at least 1 shard even if DefaultNumShards were somehow 0.
	orig := aggregate.DefaultNumShards
	aggregate.DefaultNumShards = 0
	defer func() { aggregate.DefaultNumShards = orig }()
	s := aggregate.New(0)
	ip := netip.MustParseAddr("192.0.2.100")
	// If there's at least one shard, Update won't panic.
	s.Update(ip, 100, flow.Record{Proto: flow.ProtoUDP, Packets: 1, Bytes: 100})
	bs := s.Snapshot(ip, 100, 1)
	if len(bs) != 1 || bs[0].Pps != 1 {
		t.Errorf("expected 1 bucket with Pps=1, got len=%d", len(bs))
	}
}
