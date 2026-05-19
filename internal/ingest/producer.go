package ingest

import (
	"log/slog"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v2/producer"
	protoproducer "github.com/netsampler/goflow2/v2/producer/proto"

	"github.com/mitigador/mitigador/internal/flow"
)

// decodeFunc is a package-private hook so tests can replace the goflow2-specific decode logic.
var decodeFunc = decode

// ChannelProducer implements producer.ProducerInterface.
// It gates on the Inventory (TELE-05), applies sample_rate_override (TELE-04),
// and emits flow.Record values to an output channel without blocking the caller.
type ChannelProducer struct {
	inv    *Inventory
	health *HealthTracker
	out    chan<- flow.Record
	drops  atomic.Uint64
}

// NewChannelProducer constructs the adapter.
func NewChannelProducer(inv *Inventory, health *HealthTracker, out chan<- flow.Record) *ChannelProducer {
	return &ChannelProducer{inv: inv, health: health, out: out}
}

// Drops returns the cumulative count of records dropped because the output channel was full.
// Plan 10 exposes this via /metrics; plan 12 logs warnings when it climbs.
func (p *ChannelProducer) Drops() uint64 { return p.drops.Load() }

// Produce decodes a goflow2 message into one or more flow.Record values,
// applies TELE-05 (drop unknown exporter) and TELE-04 (sample rate override),
// and forwards records on the output channel. Non-blocking: drops on backpressure.
func (p *ChannelProducer) Produce(msg interface{}, args *producer.ProduceArgs) ([]producer.ProducerMessage, error) {
	if args == nil || !args.SamplerAddress.IsValid() {
		return nil, nil
	}
	// TELE-05: gate on known exporter.
	exp, ok := p.inv.Lookup(args.SamplerAddress)
	if !ok {
		p.inv.LogUnknown(args.SamplerAddress)
		return nil, nil
	}

	records := decodeFunc(msg, exp, args)
	for i := range records {
		r := &records[i]
		// TELE-04: if the exporter has a sample_rate_override, it takes precedence over
		// the router-announced rate (Mikrotik byte-order bug workaround).
		if exp.SampleRateOverride > 0 {
			r.SampleRate = exp.SampleRateOverride
			r.Bytes *= uint64(exp.SampleRateOverride)
			r.Packets *= uint64(exp.SampleRateOverride)
		} else if r.SampleRate > 1 {
			r.Bytes *= uint64(r.SampleRate)
			r.Packets *= uint64(r.SampleRate)
		}

		select {
		case p.out <- *r:
			p.health.Observe(args.SamplerAddress, r.Received)
		default:
			d := p.drops.Add(1)
			// Log every 1000th drop to keep noise manageable.
			if d%1000 == 1 {
				slog.Warn("flow records dropped: output channel full", "drops_total", d)
			}
		}
	}
	return nil, nil
}

// Commit is a no-op: we do not pool ProtoProducerMessage instances.
func (p *ChannelProducer) Commit([]producer.ProducerMessage) {}

// Close is a no-op.
func (p *ChannelProducer) Close() {}

// decode converts a goflow2 message into one or more flow.Record values.
//
// Confirmed via go doc and source inspection of goflow2 v2.2.6:
// The concrete message types passed to Produce by goflow2's pipe layer are:
//   - *protoproducer.ProtoProducerMessage — via our delegate path (see below)
//   - *netflowlegacy.PacketNetFlowV5, *netflow.NFv9Packet, *netflow.IPFIXPacket, *sflow.Packet
//     (raw decoded structs from utils.NetFlowPipe / utils.SFlowPipe)
//
// Strategy: if the message is already a *ProtoProducerMessage (from a nested producer),
// extract fields directly. Otherwise, fall back to delegating to a goflow2 ProtoProducer
// to decode the raw packet structs into ProtoProducerMessage, then extract from those.
// This keeps our decode logic free of duplicating goflow2's NetFlow/IPFIX/sFlow parsing.
func decode(msg interface{}, exp *Exporter, args *producer.ProduceArgs) []flow.Record {
	switch m := msg.(type) {
	case *protoproducer.ProtoProducerMessage:
		// Message was already decoded by goflow2's proto layer or passed directly.
		return []flow.Record{extractRecord(m, exp.SourceIP, args.TimeReceived)}

	default:
		// Raw packet struct from goflow2's pipe layer. Delegate to the proto producer.
		// goflow2 requires a non-nil ProtoProducerConfig — passing nil panics inside
		// enrich() at p.cfg.GetFormatter(). An empty ProducerConfig{} compiled to defaults
		// is what cmd/goflow2 itself uses when no --mapping is provided.
		cfgProducer := &protoproducer.ProducerConfig{}
		cfgm, err := cfgProducer.Compile()
		if err != nil {
			slog.Warn("decode: failed to compile proto producer config", "err", err.Error())
			return nil
		}
		pp, err := protoproducer.CreateProtoProducer(
			cfgm,
			protoproducer.CreateSamplingSystem,
		)
		if err != nil {
			slog.Warn("decode: failed to create proto producer", "err", err.Error())
			return nil
		}
		protoArgs := &producer.ProduceArgs{
			Src:            args.Src,
			Dst:            args.Dst,
			SamplerAddress: args.SamplerAddress,
			TimeReceived:   args.TimeReceived,
		}
		msgs, err := pp.Produce(m, protoArgs)
		if err != nil {
			// Non-fatal: malformed packet or unrecognised format; log at debug level.
			slog.Debug("decode: proto producer error", "err", err.Error())
			return nil
		}
		pp.Commit(msgs)

		records := make([]flow.Record, 0, len(msgs))
		for _, raw := range msgs {
			pm, ok := raw.(*protoproducer.ProtoProducerMessage)
			if !ok {
				continue
			}
			records = append(records, extractRecord(pm, exp.SourceIP, args.TimeReceived))
		}
		return records
	}
}

// extractRecord maps the fields of a ProtoProducerMessage into a flow.Record.
//
// Field mapping confirmed from flowpb.FlowMessage (pb/flow.proto in goflow2 v2.2.6):
//   - SrcAddr []byte  → flow.Record.SrcIP (4 or 16 bytes)
//   - DstAddr []byte  → flow.Record.DstIP
//   - Proto   uint32  → flow.Record.Proto (IANA protocol number)
//   - Bytes   uint64  → flow.Record.Bytes  (raw sampled count; expansion happens in Produce)
//   - Packets uint64  → flow.Record.Packets
//   - SamplingRate uint64 → flow.Record.SampleRate
func extractRecord(m *protoproducer.ProtoProducerMessage, exporterIP netip.Addr, received time.Time) flow.Record {
	if received.IsZero() {
		received = time.Now()
	}
	r := flow.Record{
		Exporter:   exporterIP,
		Proto:      flow.Proto(m.Proto),
		Bytes:      m.Bytes,
		Packets:    m.Packets,
		SampleRate: uint32(m.SamplingRate),
		Received:   received,
	}
	// SrcAddr and DstAddr are raw big-endian bytes (4 bytes for IPv4, 16 for IPv6).
	if addr, ok := bytesToAddr(m.SrcAddr); ok {
		r.SrcIP = addr
	}
	if addr, ok := bytesToAddr(m.DstAddr); ok {
		r.DstIP = addr
	}
	return r
}

// bytesToAddr converts a raw 4- or 16-byte address slice into a netip.Addr.
func bytesToAddr(b []byte) (netip.Addr, bool) {
	switch len(b) {
	case 4:
		return netip.AddrFrom4([4]byte{b[0], b[1], b[2], b[3]}), true
	case 16:
		var a16 [16]byte
		copy(a16[:], b)
		return netip.AddrFrom16(a16), true
	}
	return netip.Addr{}, false
}
