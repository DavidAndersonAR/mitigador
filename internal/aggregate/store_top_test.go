package aggregate

import (
	"net/netip"
	"sync"
	"testing"

	"github.com/mitigador/mitigador/internal/flow"
)

// feed is a test helper that pushes traffic for one host into the store at the given second.
// bytes/packets placed into a single bucket at "now" — sufficient for ordering and proto tests.
func feed(t *testing.T, s *Store, ip string, now int64, packets, bytes uint64, proto flow.Proto) {
	t.Helper()
	addr := netip.MustParseAddr(ip)
	s.Update(addr, now, flow.Record{
		DstIP:   addr,
		Packets: packets,
		Bytes:   bytes,
		Proto:   proto,
	})
}

func TestStore_Top_EmptyStore_ReturnsEmpty(t *testing.T) {
	s := New(1)
	got := s.Top(100, 20)
	if got == nil {
		t.Fatal("Top must return empty slice, not nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 entries, got %d", len(got))
	}
}

func TestStore_Top_NLessThanOrEqualZero_ReturnsEmpty(t *testing.T) {
	s := New(1)
	feed(t, s, "10.0.0.1", 100, 10, 1000, flow.ProtoUDP)
	for _, n := range []int{0, -1, -100} {
		got := s.Top(100, n)
		if len(got) != 0 {
			t.Fatalf("n=%d: want 0 entries, got %d", n, len(got))
		}
	}
}

func TestStore_Top_OrdersByBpsDesc(t *testing.T) {
	s := New(2)
	feed(t, s, "10.0.0.1", 100, 1, 1000, flow.ProtoUDP)
	feed(t, s, "10.0.0.2", 100, 1, 9000, flow.ProtoUDP)
	feed(t, s, "10.0.0.3", 100, 1, 5000, flow.ProtoUDP)
	got := s.Top(100, 10)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[0].IP.String() != "10.0.0.2" || got[1].IP.String() != "10.0.0.3" || got[2].IP.String() != "10.0.0.1" {
		t.Fatalf("wrong order: %+v", got)
	}
}

func TestStore_Top_RespectsN(t *testing.T) {
	s := New(4)
	for i := 1; i <= 25; i++ {
		feed(t, s, "10.0.0."+itoaTop(i), 100, 1, uint64(i)*1000, flow.ProtoUDP)
	}
	got := s.Top(100, 20)
	if len(got) != 20 {
		t.Fatalf("want 20, got %d", len(got))
	}
	// Highest bps first — host i=25 contributed 25000 bytes.
	if got[0].Bps != 25000 {
		t.Fatalf("want top bps=25000, got %d", got[0].Bps)
	}
}

func TestStore_Top_ExcludesZeroBpsHosts(t *testing.T) {
	s := New(1)
	addr := netip.MustParseAddr("10.0.0.1")
	// Update sets LastSec but with zero bytes/packets.
	s.Update(addr, 100, flow.Record{DstIP: addr, Packets: 0, Bytes: 0, Proto: flow.ProtoUDP})
	got := s.Top(100, 20)
	if len(got) != 0 {
		t.Fatalf("zero-bps host must be excluded, got %d entries: %+v", len(got), got)
	}
}

func TestStore_Top_ExcludesStaleHosts(t *testing.T) {
	s := New(1)
	feed(t, s, "10.0.0.1", 100, 1, 1000, flow.ProtoUDP)
	// now = 100 + WindowSize + 1 → host is stale (now - LastSec > WindowSize)
	got := s.Top(100+int64(WindowSize)+1, 20)
	if len(got) != 0 {
		t.Fatalf("stale host must be excluded, got %d entries: %+v", len(got), got)
	}
}

func TestStore_Top_DominantProto(t *testing.T) {
	cases := []struct {
		name       string
		udpBytes   uint64
		icmpBytes  uint64
		otherProto flow.Proto
		otherBytes uint64
		want       string
	}{
		{"udp_majority", 8000, 2000, flow.ProtoTCP, 1000, "udp"},
		{"icmp_majority", 1000, 5000, flow.ProtoTCP, 2000, "icmp"},
		{"other_majority", 1000, 1000, flow.ProtoTCP, 9000, "other"},
		{"tie_udp_icmp_prefers_udp", 5000, 5000, flow.ProtoTCP, 0, "udp"},
		{"tie_icmp_other_prefers_icmp", 0, 5000, flow.ProtoTCP, 5000, "icmp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(1)
			addr := netip.MustParseAddr("10.0.0.10")
			if tc.udpBytes > 0 {
				s.Update(addr, 100, flow.Record{DstIP: addr, Packets: 1, Bytes: tc.udpBytes, Proto: flow.ProtoUDP})
			}
			if tc.icmpBytes > 0 {
				s.Update(addr, 100, flow.Record{DstIP: addr, Packets: 1, Bytes: tc.icmpBytes, Proto: flow.ProtoICMP})
			}
			if tc.otherBytes > 0 {
				s.Update(addr, 100, flow.Record{DstIP: addr, Packets: 1, Bytes: tc.otherBytes, Proto: tc.otherProto})
			}
			got := s.Top(100, 20)
			if len(got) != 1 {
				t.Fatalf("want 1, got %d", len(got))
			}
			if got[0].DominantProto != tc.want {
				t.Fatalf("want proto=%s, got %s", tc.want, got[0].DominantProto)
			}
		})
	}
}

func TestStore_Top_TieBreakByIPAsc(t *testing.T) {
	s := New(2)
	feed(t, s, "10.0.0.5", 100, 1, 5000, flow.ProtoUDP)
	feed(t, s, "10.0.0.2", 100, 1, 5000, flow.ProtoUDP)
	feed(t, s, "10.0.0.9", 100, 1, 5000, flow.ProtoUDP)
	got := s.Top(100, 10)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[0].IP.String() != "10.0.0.2" || got[1].IP.String() != "10.0.0.5" || got[2].IP.String() != "10.0.0.9" {
		t.Fatalf("tie-break broken: %+v", got)
	}
}

func TestStore_Top_RaceSafety(t *testing.T) {
	s := New(4)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			addr := netip.MustParseAddr("10.0.0." + itoaTop(id+1))
			for i := 0; i < 100; i++ {
				s.Update(addr, 100, flow.Record{DstIP: addr, Packets: 1, Bytes: 1000, Proto: flow.ProtoUDP})
			}
		}(w)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = s.Top(100, 20)
		}
	}()
	wg.Wait()
}

// itoaTop converts a non-negative int to a decimal string without importing strconv.
func itoaTop(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
