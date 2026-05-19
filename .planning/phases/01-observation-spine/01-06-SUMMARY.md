---
phase: 01
plan: 06
subsystem: aggregate
tags: [ring-buffer, sharded-map, sliding-window, hot-path, ram-only, concurrency]
dependency_graph:
  requires: [01-01, 01-05-flow-record-type]
  provides: [aggregate-store, per-host-counters, sliding-window, tick-eviction]
  affects: [01-07]
tech_stack:
  added:
    - "hash/fnv (stdlib) — FNV-1a for IP-to-shard routing"
    - "net/netip (stdlib) — netip.Addr as map key (comparable, zero-alloc)"
    - "sync.Mutex per shard — fine-grained locking"
  patterns:
    - "Sharded map with FNV-1a hash routing for lock-free cross-shard parallelism"
    - "Ring buffer with overwrite-next-slot Tick for O(1) sliding window"
    - "TDD: RED commit then GREEN implementation"
key_files:
  created:
    - internal/aggregate/bucket.go
    - internal/aggregate/ring.go
    - internal/aggregate/shard.go
    - internal/aggregate/store.go
    - internal/aggregate/ring_test.go
    - internal/aggregate/store_test.go
    - internal/flow/record.go
  modified: []
decisions:
  - "FNV-1a on ip.As16() (not ip.AsSlice()) for both IPv4 and IPv6 uniformity — As16() is always 16 bytes, avoiding nil-slice risk on v4-mapped addresses"
  - "LastSec updated only if sec > hr.LastSec (monotonic guard) — prevents stale flows from clobbering the eviction clock"
  - "ICMPv6 (58) classified alongside ICMP (1) in per-proto breakdown — both are ICMP family for DDoS purposes"
  - "flow.Record minimal stub created in internal/flow/record.go (Rule 3: blocking issue) — plan 01-05 running in parallel had not yet created it; stub defines the exact types needed by aggregate"
metrics:
  duration_seconds: 480
  completed_date: "2026-05-19"
  tasks_completed: 2
  files_created: 7
  files_modified: 0
---

# Phase 1 Plan 6: Aggregate Store Summary

**One-liner:** Sharded per-host 60-slot ring-buffer counter store (FNV-1a routing, mutex-per-shard, Tick-based cold eviction) providing race-free pps/bps/proto-breakdown snapshots for the detection engine.

## What Was Built

### Store API (exported)

| Symbol | Kind | Signature |
|--------|------|-----------|
| `DefaultNumShards` | `var` | `int` (= `runtime.NumCPU()`) |
| `WindowSize` | `const` | `= 60` |
| `Bucket` | `struct` | `Pps, Bps, PpsUDP, BpsUDP, PpsICMP, BpsICMP, PpsOther, BpsOther uint64` |
| `HostRing` | `struct` | `Buckets [60]Bucket; LastSec int64` |
| `Shard` | `struct` | `mu sync.Mutex; hosts map[netip.Addr]*HostRing` (unexported fields) |
| `Store` | `struct` | `shards []*Shard; n uint32` (unexported fields) |
| `HostInfo` | `struct` | `IP netip.Addr; LastSec int64` |
| `New(numShards int) *Store` | func | Creates store with N shards (min 1) |
| `(*Store).Update(ip, sec, flow.Record)` | method | Merges record into ring slot `sec % 60`, updates `LastSec` |
| `(*Store).Snapshot(ip, now, window) []Bucket` | method | Returns `window` buckets newest-first; nil if unknown host |
| `(*Store).Tick(now int64)` | method | Zeroes `(now+1) % 60` slot; cold-evicts hosts with `now - LastSec > 60` |
| `(*Store).ActiveHosts(now int64) []HostInfo` | method | Returns hosts with `now - LastSec <= 60` |

### Key Design Details

**Sharding:** `FNV-1a(ip.As16()) % N` routes each IP to one of N shards. Lock contention is bounded per shard — an attack targeting one /32 holds one shard's mutex for microseconds at typical sample rates.

**Ring slot:** `sec % WindowSize` (with positive-modulo correction for hypothetical negative seconds). Multiple flows targeting the same second accumulate atomically within the same mutex hold.

**Sliding window (Tick):** Rather than a full scan at read time, Tick pre-zeros the slot that will be written next second. This means `Snapshot` reads stale data only if `Tick` was skipped — which is the detector's responsibility (plan 07 must call `Tick` at the same 1Hz cadence as its own evaluation).

**Cold eviction:** `now - hr.LastSec > WindowSize` (strictly greater). A host last seen exactly 60 seconds ago is still kept — the data in its ring is still within the window.

### Memory Budget

- Per host: `60 × 8 fields × 8 bytes = 3,840 bytes ≈ 3.75 KB`
- Typical ISP /22 active hosts at any moment: ~1,000 → ~3.75 MB total
- Worst-case flood (1M distinct /32 targets): ~3.75 GB — operator should monitor `len(ActiveHosts(now))` via a Prometheus gauge (plan 10)

### WindowSize Value

`const WindowSize = 60` — one second per slot, 60 slots = 60-second sliding window.

### Default Shard Count

`var DefaultNumShards = runtime.NumCPU()` — scales automatically with hardware concurrency. Override via `New(n)`.

### PERS-03 / PERS-04 Confirmation

No file I/O, no database writes anywhere in this package:

- `grep -rn "os.Create\|sql.*Exec\|pgx.*Exec\|.WriteFile\|.WriteString" internal/aggregate/` → **0 hits**
- Package imports: `hash/fnv`, `net/netip`, `runtime`, `sync`, `github.com/mitigador/mitigador/internal/flow` — no `os`, `database/sql`, `pgx`

**PERS-03 (RAM-only counters):** Satisfied — all counters live in `map[netip.Addr]*HostRing` in process memory.

**PERS-04 (no raw-flow tables):** Not applicable to this package, but confirmed no DB calls exist.

### /32 Scope (Phase 1 Only)

This plan implements /32 bucket keys only (one entry per `netip.Addr`). Carpet-bombing aggregation (/28, /24, /22 prefix rollup) is Phase 3 (DETE-04, DETE-07, TELE-07) and is out of scope here.

### Note for Plan 07 (Detect)

The detector should share a single `time.Ticker` for both its 1Hz evaluation loop and `store.Tick(now)`. Calling `Tick` and `Snapshot`/`ActiveHosts` in the same goroutine iteration is the correct pattern — no additional synchronization is required beyond the per-shard mutexes already in place.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Created minimal flow.Record stub**
- **Found during:** Task 1 (GREEN phase)
- **Issue:** `internal/flow/record.go` did not exist — plan 01-05 is running in parallel and had not yet created it. Without the `flow.Record` type, the `aggregate` package cannot compile.
- **Fix:** Created `internal/flow/record.go` with `Record` struct (`SrcAddr`, `DstAddr`, `SrcPort`, `DstPort`, `Proto`, `Packets`, `Bytes`, `Received`) and protocol constants (`ProtoICMP=1`, `ProtoTCP=6`, `ProtoUDP=17`, `ProtoICMPv6=58`). This is the minimal surface the aggregate package and its tests require.
- **Impact:** Plan 01-05 will extend this file. If 01-05 defines a different set of fields, it should be backward-compatible since aggregate only reads `Proto`, `Packets`, `Bytes` from the Record. If there is a conflict, the plan 01-05 executor should merge — the fields here are the canonical minimum.
- **Files modified:** `internal/flow/record.go` (created)
- **Commit:** `1d77da0`

**2. [Rule 1 - Bug] Tick eviction boundary: `>` not `>=`**
- **Found during:** Task 2 (test design)
- **Issue:** The plan spec says "hosts not seen in `WindowSize` seconds" — a host last seen exactly 60 seconds ago still has valid data in the ring (the bucket at that slot). Using `>=` would incorrectly evict a host that still has data.
- **Fix:** Used `now - hr.LastSec > WindowSize` (strictly greater) so a host at exactly the boundary is kept for one more Tick cycle.
- **Commit:** `1d77da0`

**3. ICMPv6 protocol classification**
- **Found during:** Task 1 (implementation)
- **Plan said:** Two cases — `ProtoUDP` and `ProtoICMP`.
- **Fix:** Added `flow.ProtoICMPv6` (58) to the ICMP case — both protocol numbers represent the ICMP family and should contribute to the same DDoS attack vector counter.
- **Files modified:** `internal/aggregate/store.go`
- **Commit:** `1d77da0`

## Known Stubs

None — all aggregate package functionality is fully implemented and tested.

## Threat Flags

No new threat surface beyond the plan's threat model. The aggregate package has no network endpoints, auth paths, file access, or schema changes.

## Self-Check: PASSED

Files created:
- FOUND: internal/aggregate/bucket.go
- FOUND: internal/aggregate/ring.go
- FOUND: internal/aggregate/shard.go
- FOUND: internal/aggregate/store.go
- FOUND: internal/aggregate/ring_test.go
- FOUND: internal/aggregate/store_test.go
- FOUND: internal/flow/record.go

Commits:
- RED: `git log --oneline | grep "test(01-06)"` → found (ring_test.go failing tests)
- GREEN Task 1: `1d77da0` — feat(01-06): implement aggregate store — Bucket, HostRing, Shard, Store Update/Snapshot
- GREEN Task 2: `d297728` — feat(01-06): add Tick, ActiveHosts, and store_test.go

Verifications:
- `go build ./...` exits 0
- `go vet ./internal/aggregate/...` exits 0
- `go test -race ./internal/aggregate/... -count=1` → 14 tests pass
- PERS-04 guard: `grep -rn "os.Create|sql.*Exec|..."` → 0 hits
