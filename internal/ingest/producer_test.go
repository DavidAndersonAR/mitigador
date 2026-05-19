package ingest

import (
	"net/netip"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v2/producer"
	protoproducer "github.com/netsampler/goflow2/v2/producer/proto"

	"github.com/mitigador/mitigador/internal/flow"
	"golang.org/x/time/rate"
)

// mkInv builds a minimal Inventory for unit tests, without Postgres.
func mkInv(ip string, override uint32) *Inventory {
	addr := netip.MustParseAddr(ip)
	inv := &Inventory{
		byIP:       make(map[netip.Addr]*Exporter),
		unknownLim: make(map[netip.Addr]*rate.Limiter),
	}
	inv.byIP[addr] = &Exporter{ID: 1, SourceIP: addr, Type: "netflow", SampleRateOverride: override}
	return inv
}

func TestProduce_KnownExporter_EmitsRecord(t *testing.T) {
	ip := netip.MustParseAddr("10.0.0.1")
	inv := mkInv("10.0.0.1", 0)
	h := NewHealthTracker()
	out := make(chan flow.Record, 1)
	p := NewChannelProducer(inv, h, out)

	// Override decodeFunc to emit one synthetic record.
	orig := decodeFunc
	defer func() { decodeFunc = orig }()
	decodeFunc = func(msg interface{}, e *Exporter, args *producer.ProduceArgs) []flow.Record {
		return []flow.Record{{
			Exporter: args.SamplerAddress,
			DstIP:    netip.MustParseAddr("192.0.2.10"),
			Proto:    flow.ProtoUDP,
			Bytes:    100,
			Packets:  1,
			Received: time.Now(),
		}}
	}
	_, _ = p.Produce(nil, &producer.ProduceArgs{SamplerAddress: ip})
	select {
	case r := <-out:
		if r.Bytes != 100 || r.Packets != 1 {
			t.Errorf("got %+v", r)
		}
	default:
		t.Fatal("expected record on channel")
	}
}

func TestProduce_UnknownExporter_DropsRecord(t *testing.T) {
	inv := mkInv("10.0.0.1", 0)
	h := NewHealthTracker()
	out := make(chan flow.Record, 1)
	p := NewChannelProducer(inv, h, out)

	orig := decodeFunc
	defer func() { decodeFunc = orig }()
	called := false
	decodeFunc = func(msg interface{}, e *Exporter, args *producer.ProduceArgs) []flow.Record {
		called = true
		return []flow.Record{{Exporter: args.SamplerAddress, DstIP: netip.MustParseAddr("192.0.2.10"), Bytes: 100, Packets: 1}}
	}
	_, _ = p.Produce(nil, &producer.ProduceArgs{SamplerAddress: netip.MustParseAddr("10.99.99.99")})
	if called {
		t.Error("decode was called for unknown exporter")
	}
	select {
	case r := <-out:
		t.Fatalf("unexpected record emitted: %+v", r)
	default:
		// OK
	}
}

func TestProduce_SampleRateOverride_AppliesAndExpands(t *testing.T) {
	ip := netip.MustParseAddr("10.0.0.1")
	inv := mkInv("10.0.0.1", 1000)
	h := NewHealthTracker()
	out := make(chan flow.Record, 1)
	p := NewChannelProducer(inv, h, out)

	orig := decodeFunc
	defer func() { decodeFunc = orig }()
	decodeFunc = func(msg interface{}, e *Exporter, args *producer.ProduceArgs) []flow.Record {
		return []flow.Record{{
			Exporter:   args.SamplerAddress,
			DstIP:      netip.MustParseAddr("192.0.2.10"),
			Bytes:      100,
			Packets:    1,
			SampleRate: 1,
			Received:   time.Now(),
		}}
	}
	_, _ = p.Produce(nil, &producer.ProduceArgs{SamplerAddress: ip})
	r := <-out
	if r.SampleRate != 1000 {
		t.Errorf("SampleRate = %d, want 1000", r.SampleRate)
	}
	if r.Bytes != 100000 {
		t.Errorf("Bytes = %d, want 100000", r.Bytes)
	}
	if r.Packets != 1000 {
		t.Errorf("Packets = %d, want 1000", r.Packets)
	}
}

func TestProduce_BackpressureIncrementsDrops(t *testing.T) {
	ip := netip.MustParseAddr("10.0.0.1")
	inv := mkInv("10.0.0.1", 0)
	h := NewHealthTracker()
	// unbuffered → blocks on send → triggers default branch
	out := make(chan flow.Record)
	p := NewChannelProducer(inv, h, out)

	orig := decodeFunc
	defer func() { decodeFunc = orig }()
	decodeFunc = func(msg interface{}, e *Exporter, args *producer.ProduceArgs) []flow.Record {
		return []flow.Record{{Exporter: args.SamplerAddress, DstIP: netip.MustParseAddr("192.0.2.10"), Bytes: 100, Packets: 1}}
	}
	_, _ = p.Produce(nil, &producer.ProduceArgs{SamplerAddress: ip})
	if p.Drops() != 1 {
		t.Errorf("Drops = %d, want 1", p.Drops())
	}
}

// TestDecode_RealMessage tests the actual goflow2 v2.2.6 decode path using
// a *protoproducer.ProtoProducerMessage constructed by the protoproducer.
// This validates the D-02 requirement: decode must work for real goflow2 messages.
func TestDecode_RealMessage(t *testing.T) {
	// Build a ProtoProducerMessage directly to simulate what the proto producer returns.
	// This is the type that our decode() function receives via the delegate path.
	msg := &protoproducer.ProtoProducerMessage{}
	msg.SrcAddr = []byte{192, 0, 2, 1}      // 192.0.2.1
	msg.DstAddr = []byte{198, 51, 100, 10}   // 198.51.100.10
	msg.Proto = 17                            // UDP
	msg.SrcPort = 12345
	msg.DstPort = 53
	msg.Bytes = 200
	msg.Packets = 2
	msg.SamplingRate = 100

	samplerIP := netip.MustParseAddr("10.0.0.1")
	exp := &Exporter{ID: 1, SourceIP: samplerIP, Type: "netflow"}
	args := &producer.ProduceArgs{SamplerAddress: samplerIP}

	records := decode(msg, exp, args)
	if len(records) < 1 {
		t.Fatalf("decode returned 0 records from a real ProtoProducerMessage; D-02 requires non-empty output")
	}
	r := records[0]
	if !r.DstIP.IsValid() || r.DstIP.IsUnspecified() {
		t.Errorf("DstIP not populated: %v", r.DstIP)
	}
	if !r.SrcIP.IsValid() || r.SrcIP.IsUnspecified() {
		t.Errorf("SrcIP not populated: %v", r.SrcIP)
	}
	if r.Proto != flow.ProtoUDP {
		t.Errorf("Proto = %d, want ProtoUDP(17)", r.Proto)
	}
	if r.Bytes != 200 {
		t.Errorf("Bytes = %d, want 200", r.Bytes)
	}
	if r.Packets != 2 {
		t.Errorf("Packets = %d, want 2", r.Packets)
	}
	if r.SampleRate != 100 {
		t.Errorf("SampleRate = %d, want 100", r.SampleRate)
	}
	if r.Exporter != samplerIP {
		t.Errorf("Exporter = %v, want %v", r.Exporter, samplerIP)
	}
	if r.Received.IsZero() {
		t.Errorf("Received should not be zero")
	}
}
