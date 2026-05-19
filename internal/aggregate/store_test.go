package aggregate_test

import (
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/aggregate"
	"github.com/mitigador/mitigador/internal/flow"
)

func TestStore_Tick_ZeroesNextBucket(t *testing.T) {
	s := aggregate.New(4)
	ip := netip.MustParseAddr("192.0.2.10")
	// Update bucket at sec=0 (index 0).
	s.Update(ip, 0, flow.Record{Proto: flow.ProtoUDP, Packets: 10, Bytes: 1000})
	// Tick at now=0 zeros the NEXT slot (index 1), not the current slot (index 0).
	s.Tick(0)
	// Bucket at sec=0 (index 0) must still have the data.
	bs := s.Snapshot(ip, 0, 1)
	if len(bs) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(bs))
	}
	if bs[0].Pps != 10 {
		t.Errorf("Tick incorrectly zeroed current slot: Pps=%d, want 10", bs[0].Pps)
	}
	// Bucket at sec=1 (index 1) must be zero (Tick zeroed it).
	bs2 := s.Snapshot(ip, 1, 1)
	if bs2[0].Pps != 0 {
		t.Errorf("next slot not zeroed by Tick: Pps=%d, want 0", bs2[0].Pps)
	}
}

func TestStore_Tick_EvictsColdHosts(t *testing.T) {
	s := aggregate.New(4)
	ip := netip.MustParseAddr("192.0.2.11")
	// Update at sec=0.
	s.Update(ip, 0, flow.Record{Proto: flow.ProtoUDP, Packets: 10, Bytes: 1000})
	// Tick at now=61 — host's LastSec=0 is now-0=61 > WindowSize(60), so evict.
	s.Tick(61)
	// Host should be gone.
	bs := s.Snapshot(ip, 61, 1)
	if bs != nil {
		t.Errorf("expected nil after cold eviction, got %v", bs)
	}
}

func TestStore_Tick_KeepsRecentHosts(t *testing.T) {
	s := aggregate.New(4)
	ip := netip.MustParseAddr("192.0.2.12")
	// Update at sec=0.
	s.Update(ip, 0, flow.Record{Proto: flow.ProtoUDP, Packets: 10, Bytes: 1000})
	// Tick at now=30 — host's LastSec=0 is now-0=30 <= WindowSize(60), so keep.
	s.Tick(30)
	// Host must still be present (Snapshot for 60-bucket window returns 60 buckets).
	bs := s.Snapshot(ip, 30, aggregate.WindowSize)
	if len(bs) != aggregate.WindowSize {
		t.Fatalf("expected %d buckets, got %d", aggregate.WindowSize, len(bs))
	}
}

func TestStore_ActiveHosts_ReturnsOnlyRecent(t *testing.T) {
	s := aggregate.New(4)
	ipA := netip.MustParseAddr("192.0.2.20")
	ipB := netip.MustParseAddr("192.0.2.21")
	// ipA updated at sec=0, ipB at sec=100.
	s.Update(ipA, 0, flow.Record{Proto: flow.ProtoUDP, Packets: 1, Bytes: 100})
	s.Update(ipB, 100, flow.Record{Proto: flow.ProtoUDP, Packets: 1, Bytes: 100})
	// ActiveHosts at now=120: ipA.LastSec=0, now-0=120 > 60 → excluded.
	// ipB.LastSec=100, now-100=20 <= 60 → included.
	hosts := s.ActiveHosts(120)
	if len(hosts) != 1 {
		t.Fatalf("expected 1 active host, got %d", len(hosts))
	}
	if hosts[0].IP != ipB {
		t.Errorf("expected ipB, got %v", hosts[0].IP)
	}
}

func TestStore_TickAndUpdate_NoRace(t *testing.T) {
	s := aggregate.New(8)
	ip := netip.MustParseAddr("192.0.2.30")
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer goroutine: Update continuously for ~100ms.
	wg.Add(1)
	go func() {
		defer wg.Done()
		sec := int64(0)
		for {
			select {
			case <-stop:
				return
			default:
				s.Update(ip, sec%aggregate.WindowSize, flow.Record{
					Proto: flow.ProtoUDP, Packets: 1, Bytes: 100,
					Received: time.Unix(sec, 0),
				})
				sec++
			}
		}
	}()

	// Ticker goroutine: Tick continuously for ~100ms.
	wg.Add(1)
	go func() {
		defer wg.Done()
		sec := int64(0)
		for {
			select {
			case <-stop:
				return
			default:
				s.Tick(sec)
				sec++
			}
		}
	}()

	// Let them race for a short duration.
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
	// If we reach here without panicking, the race detector is satisfied.
}
