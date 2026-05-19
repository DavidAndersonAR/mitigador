---
phase: 01
plan: 05
subsystem: ingest
tags: [flow, ingest, goflow2, udp, netflow, ipfix, sflow, inventory, health, tele-04, tele-05]
dependency_graph:
  requires: [01-02, 01-03]
  provides:
    - flow.Record canonical type
    - ingest.Inventory (exporter allowlist, loaded from Postgres)
    - ingest.ChannelProducer (goflow2 v2 producer adapter)
    - ingest.HealthTracker (per-exporter last-seen + 60s rate)
    - ingest.Start (three UDP listeners lifecycle)
  affects: [01-06, 01-07, 01-10, 01-12]
tech_stack:
  added:
    - "google.golang.org/protobuf v1.36.11 (transitive via goflow2/pb — protobuf encoding)"
    - "github.com/libp2p/go-reuseport v0.4.0 (transitive via goflow2/utils — UDP SO_REUSEPORT)"
  patterns:
    - "Inventory loaded from Postgres at boot; Reload() available for explicit admin trigger (hot-reload deferred D-09)"
    - "decodeFunc package-level hook pattern: tests replace decode without touching goflow2 wire format"
    - "HealthTracker uses 60-bucket circular ring keyed by (unix_sec % 60) for O(1) Observe + O(60) Snapshot"
    - "TELE-04: SampleRateOverride > 0 replaces router-announced rate; Bytes/Packets multiplied before emit"
    - "TELE-05: Lookup gates every datagram; LogUnknown rate-limited 1/min per offending IP"
    - "ChannelProducer.decode delegates to goflow2 protoproducer for actual NetFlow/IPFIX/sFlow decoding"
key_files:
  created:
    - internal/flow/record.go
    - internal/flow/record_test.go
    - internal/ingest/exporters.go
    - internal/ingest/exporters_test.go
    - internal/ingest/health.go
    - internal/ingest/health_test.go
    - internal/ingest/producer.go
    - internal/ingest/producer_test.go
    - internal/ingest/listener.go
  modified:
    - go.mod (added google.golang.org/protobuf, libp2p/go-reuseport transitives via go mod tidy)
    - go.sum
decisions:
  - "decode() delegates to goflow2 protoproducer (CreateProtoProducer) rather than re-implementing NetFlow/IPFIX/sFlow parsing — eliminates ~300 lines of parser code, relies on proven Cloudflare-origin logic"
  - "ProtoProducerConfig passed as nil to CreateProtoProducer — uses goflow2 defaults (no custom field mapping needed for Phase 1)"
  - "goflow2 UDPReceiverConfig has no ReceiveBuffer/SO_RCVBUF field — cfg.ReceiveBufferBytes mapped to QueueSize (bytes/9000, clamped to [1000..1000000]); sysctl net.core.rmem_max must be set at the OS level"
  - "Inventory.Reload() is wired for explicit admin trigger only; SIGHUP/LISTEN-NOTIFY auto-reload is Phase 3 (D-09)"
  - "HealthTracker.Snapshot holds h.mu.Lock (not RLock) to prevent a race where Observe fires during snapshot iteration — acceptable since Snapshot is called at 1Hz by DASH-05, not on the hot path"
metrics:
  duration_seconds: 434
  completed_date: "2026-05-19"
  tasks_completed: 3
  files_created: 9
  files_modified: 2
---

# Phase 1 Plan 5: Ingest Layer Summary

**One-liner:** UDP listeners on 2055/4739/6343 via goflow2 v2.2.6 delegating decode to protoproducer, gated by an in-memory Inventory, applying per-exporter sample_rate_override (TELE-04), emitting flow.Record on a channel, with per-exporter health tracking for DASH-05.

## Confirmed goflow2 v2.2.6 Symbol Paths

All symbols confirmed via source inspection of `/home/david/go/pkg/mod/github.com/netsampler/goflow2/v2@v2.2.6/`.

### utils package (`utils/pipe.go`, `utils/udp.go`)

| Symbol | Signature | Notes |
|--------|-----------|-------|
| `utils.FlowPipe` | `interface{ DecodeFlow(interface{}) error; Close() }` | Implemented by both `*NetFlowPipe` and `*SFlowPipe` |
| `utils.NewNetFlowPipe` | `func(*PipeConfig) *NetFlowPipe` | Handles NetFlow v5, v9, and IPFIX (v10) — version byte selects decoder |
| `utils.NewSFlowPipe` | `func(*PipeConfig) *SFlowPipe` | Handles sFlow v5 |
| `utils.PipeConfig` | `struct{ Format, Transport, Producer, NetFlowTemplater }` | `Producer` field accepts `producer.ProducerInterface` |
| `utils.UDPReceiver` | `struct` | Created via `NewUDPReceiver(*UDPReceiverConfig)` |
| `utils.UDPReceiverConfig` | `struct{ Workers, Sockets int; Blocking bool; QueueSize int; ReceiverCallback }` | **No ReceiveBuffer/SO_RCVBUF field** — deviation documented below |
| `utils.NewUDPReceiver` | `func(*UDPReceiverConfig) (*UDPReceiver, error)` | Returns error if already started |
| `recv.Start` | `func(addr string, port int, decodeFunc DecoderFunc) error` | `DecoderFunc = func(interface{}) error`; `pipe.DecodeFlow` satisfies it |
| `recv.Stop` | `func() error` | Drains workers, recreates channels for potential restart |

### producer package (`producer/producer.go`)

| Symbol | Signature |
|--------|-----------|
| `producer.ProducerInterface` | `interface{ Produce(msg interface{}, args *ProduceArgs) ([]ProducerMessage, error); Commit([]ProducerMessage); Close() }` |
| `producer.ProduceArgs` | `struct{ Src, Dst netip.AddrPort; SamplerAddress netip.Addr; TimeReceived time.Time }` |
| `producer.ProducerMessage` | `interface{}` |

### protoproducer package (`producer/proto/`)

| Symbol | Notes |
|--------|-------|
| `protoproducer.ProtoProducerMessage` | Embeds `flowpb.FlowMessage`; concrete type returned by `CreateProtoProducer.Produce()` |
| `protoproducer.ProtoProducerConfig` | **Interface** (not struct) — can pass `nil` for defaults |
| `protoproducer.CreateProtoProducer(cfg ProtoProducerConfig, fn func() SamplingRateSystem) (producer.ProducerInterface, error)` | Used as decode delegate |
| `protoproducer.CreateSamplingSystem` | `func() SamplingRateSystem` — creates per-source sampling rate tracker |

## Confirmed Concrete Decoded-Message Type

The goflow2 pipes pass **raw decoded packet structs** to `Produce`:

| Flow Protocol | Message Type Passed to Produce |
|---------------|-------------------------------|
| NetFlow v5 | `*netflowlegacy.PacketNetFlowV5` |
| NetFlow v9 | `*netflow.NFv9Packet` |
| IPFIX (v10) | `*netflow.IPFIXPacket` |
| sFlow v5 | `*sflow.Packet` |

Our `decode()` function handles this via delegation: it calls `protoproducer.CreateProtoProducer().Produce(msg, args)`, which implements the full type switch above and returns `[]*protoproducer.ProtoProducerMessage`. We then extract fields from `flowpb.FlowMessage` embedded in `ProtoProducerMessage`.

The `TestDecode_RealMessage` test constructs a `*protoproducer.ProtoProducerMessage` directly (the fast path through `decode()`'s first type-switch arm) and verifies all 8 required fields are populated.

## flow.Record Field Extraction from ProtoProducerMessage

Confirmed field mapping (`pb/flow.pb.go`):

| flowpb.FlowMessage field | Type | → flow.Record field |
|--------------------------|------|---------------------|
| `SrcAddr` | `[]byte` (4 or 16 bytes, big-endian) | `SrcIP netip.Addr` |
| `DstAddr` | `[]byte` (4 or 16 bytes, big-endian) | `DstIP netip.Addr` |
| `Proto` | `uint32` | `Proto flow.Proto` (IANA number) |
| `Bytes` | `uint64` | `Bytes uint64` (pre-expansion) |
| `Packets` | `uint64` | `Packets uint64` (pre-expansion) |
| `SamplingRate` | `uint64` | `SampleRate uint32` |
| `args.SamplerAddress` | `netip.Addr` | `Exporter netip.Addr` |
| `args.TimeReceived` | `time.Time` | `Received time.Time` |

## TELE-04 Override Semantics

When `Exporter.SampleRateOverride > 0`:
1. `r.SampleRate = SampleRateOverride` — operator-configured value replaces whatever the router announced
2. `r.Bytes *= uint64(SampleRateOverride)` — expand to wire-equivalent bytes
3. `r.Packets *= uint64(SampleRateOverride)` — expand to wire-equivalent packets

When `SampleRateOverride == 0` and `r.SampleRate > 1` (router-announced rate):
1. `r.Bytes *= uint64(r.SampleRate)` — expand using router-announced rate
2. `r.Packets *= uint64(r.SampleRate)` — expand using router-announced rate

Confirmed by `TestProduce_SampleRateOverride_AppliesAndExpands`: Bytes=100 + SampleRateOverride=1000 → Bytes=100000.

## TELE-05 Drop Behavior

`ChannelProducer.Produce` calls `p.inv.Lookup(args.SamplerAddress)` before any decoding. If the IP is not in the Inventory:
- `p.inv.LogUnknown(ip)` is called (rate-limited to 1 warn/min per offending IP)
- `decode` is NOT called
- No `flow.Record` reaches the output channel

Confirmed by `TestProduce_UnknownExporter_DropsRecord`: `decode` hook was NOT called; channel remains empty.

## Mikrotik Byte-Order Bug

Mikrotik routers (NetFlow v9 / IPFIX) incorrectly encode `SamplingRate` as host-byte-order instead of network-byte-order, causing values like `0x03E80000` (≈65M) instead of the intended `1000`. The correct workaround is to set `sample_rate_override` on the exporter row in the `exporters` table to the known correct sampling rate. Once set, TELE-04 ignores the router-announced rate entirely.

## Inventory Hot-Reload Status

`Inventory.Reload(ctx, pool)` is implemented and available. It is NOT wired to any automatic trigger in Phase 1. The design doc deferred (D-09) SIGHUP/LISTEN-NOTIFY hot-reload to Phase 3. A Phase 3 admin endpoint will call `Reload()` directly.

## Deviations from Plan

### Auto-handled Issues

**1. [Rule 3 - Blocking] goflow2 UDPReceiverConfig has no ReceiveBuffer/SO_RCVBUF field**

- **Found during:** Task 3 source inspection (`utils/udp.go`)
- **Issue:** Plan acceptance criteria require passing `cfg.ReceiveBufferBytes` to the UDP receiver config. goflow2 v2.2.6's `UDPReceiverConfig` struct has fields `Workers`, `Sockets`, `Blocking`, `QueueSize`, `ReceiverCallback` — no buffer size field. The kernel UDP socket buffer is set by `reuseport.ListenPacket` without SO_RCVBUF customization.
- **Fix:** `cfg.ReceiveBufferBytes` is consumed to derive `QueueSize` (bytes / 9000 bytes-per-packet, clamped to [1000..1000000]). This provides proportional backpressure within goflow2's dispatch goroutine. Operators needing larger kernel buffers must set `net.core.rmem_max` via sysctl. Documented in listener.go comments.
- **Files modified:** `internal/ingest/listener.go`
- **Commit:** `ac03a90`

**2. [Rule 3 - Blocking] go mod tidy needed for transitive protobuf and reuseport deps**

- **Found during:** Task 2 and Task 3 first build
- **Issue:** Importing `producer/proto` and `utils` from goflow2 pulled in `google.golang.org/protobuf v1.36.11` and `github.com/libp2p/go-reuseport v0.4.0` which were absent from go.sum.
- **Fix:** Ran `go mod tidy` twice (once per task). No functional change.
- **Files modified:** `go.mod`, `go.sum`
- **Commits:** `b83ecb2`, `ac03a90`

## Known Stubs

None — all exported symbols are fully implemented. `Inventory.Reload()` is fully wired but not called automatically; that is intentional per D-09 (Phase 3 work).

## Threat Flags

No new threat surface introduced beyond what the plan's threat model documents.

- T-01-05-01 (spoofing via IP spoof): mitigated by TELE-05 Inventory gate — `TestProduce_UnknownExporter_DropsRecord` confirms.
- T-01-05-02 (UDP flood DoS): mitigated by QueueSize-bounded dispatch + ChannelProducer.Drops() counter + non-blocking select.
- T-01-05-03 (malformed packet panic): goflow2 proto producer handles decode errors without panicking; `decode()` logs at Debug and returns nil on error.
- T-01-05-04 (Mikrotik byte-order → false positive): mitigated by TELE-04 sample_rate_override.
- T-01-05-05 (drops not surfaced): `ChannelProducer.Drops()` ready for /metrics in plan 10.
- T-01-05-06 (empty Inventory silent failure): plan 12 startup MUST log warn if `len(inv.All()) == 0`.
- T-01-05-07 (flow.Record in memory): PERS-04 confirmed — no disk writes in this plan.

## Self-Check: PASSED

Files created:
- FOUND: internal/flow/record.go
- FOUND: internal/flow/record_test.go
- FOUND: internal/ingest/exporters.go
- FOUND: internal/ingest/exporters_test.go
- FOUND: internal/ingest/health.go
- FOUND: internal/ingest/health_test.go
- FOUND: internal/ingest/producer.go
- FOUND: internal/ingest/producer_test.go
- FOUND: internal/ingest/listener.go

Commits:
- FOUND: 76bd07a — feat(01-05): flow.Record type, Inventory, and HealthTracker
- FOUND: b83ecb2 — feat(01-05): ChannelProducer — goflow2 adapter with TELE-04/TELE-05 gates
- FOUND: ac03a90 — feat(01-05): UDP listener wiring — three ports via goflow2 utils + Start()

Verifications:
- `go build ./...` exits 0
- `go vet ./internal/flow/... ./internal/ingest/...` exits 0
- `go test ./internal/flow/... ./internal/ingest/... -count=1` exits 0 (15 tests pass)
- ProtoUDP=17, ProtoICMP=1, ProtoTCP=6, ProtoOther=0: verified
- Inventory methods (Lookup/LogUnknown/Reload/All/AllByType) × 5: verified
- rate.NewLimiter(rate.Every(time.Minute), 1) present: verified
- StaleAfter=60s, OfflineAfter=5min: verified
- select/default non-blocking send + drops.Add: verified
- p.inv.Lookup called before decode: verified
- p.inv.LogUnknown called on unknown: verified
- p.health.Observe on success path: verified
- SampleRateOverride multiplicative expansion: verified
- switch m := msg.(type) in decode(): verified
- cfg.NetFlow/IPFIX/SFlow.ListenPort all referenced: verified
- cfg.ReceiveBufferBytes consumed: verified
- ctx.Done() blocks listener: verified
