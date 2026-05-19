---
phase: 01
plan: 07
subsystem: detect
tags: [detection-engine, state-machine, attack-event, threshold-catalog, vector-classify, confidence-score, ulid, 1hz-tick]
dependency_graph:
  requires: [01-02-thresholds-schema, 01-06-aggregate-store]
  provides: [attack-event-bus, detection-engine, threshold-catalog, vector-classification, confidence-score]
  affects: [01-08-incident-recorder, 01-09-alert-bus, 01-10-sse-dashboard]
tech_stack:
  added:
    - "github.com/oklog/ulid/v2 v2.1.1 — sortable, URL-safe incident IDs"
  patterns:
    - "Per-(host, vector) state machine: IDLE → ACTIVE → COOLDOWN → IDLE"
    - "Tick-count grace window: noViolationTicks >= graceTicks (not time.Sub)"
    - "Exported Key/Input/StateMachine for white-box unit testing without mocks"
    - "Non-blocking channel send with atomic dropped counter (drop-on-full backpressure)"
    - "Longest-prefix-match threshold catalog sorted descending by Prefix.Bits()"
    - "TDD: RED commit then GREEN implementation for all three tasks"
key_files:
  created:
    - internal/detect/event.go
    - internal/detect/thresholds.go
    - internal/detect/classify.go
    - internal/detect/score.go
    - internal/detect/state.go
    - internal/detect/engine.go
    - internal/detect/thresholds_test.go
    - internal/detect/classify_test.go
    - internal/detect/score_test.go
    - internal/detect/state_test.go
    - internal/detect/engine_test.go
  modified:
    - go.mod
    - go.sum
decisions:
  - "Grace window uses tick-counting (noViolationTicks >= graceTicks) rather than time.Sub — ensures exactly grace_sec consecutive non-violating 1Hz ticks are required, regardless of wall-clock drift between ticks"
  - "Confidence duration factor only applies when at least one of pps/bps is above threshold — prevents non-zero score at exactly-threshold traffic (sat(1) = 0 by design)"
  - "StateMachine exported (not internal) to allow white-box testing without a mock interface — the engine holds a *StateMachine directly; tests call Step() from a single goroutine matching production constraints"
  - "Engine.Tick(now) exported for deterministic testing — avoids time.Sleep-based tests; callers must not invoke concurrently with Run()"
  - "NewCatalogFromThresholds exported — avoids DB dependency in unit tests (same pattern as plan 06's Store.New)"
metrics:
  duration_seconds: 1800
  completed_date: "2026-05-19"
  tasks_completed: 3
  files_created: 11
  files_modified: 2
---

# Phase 1 Plan 7: Detection Engine Summary

**One-liner:** 1Hz detection engine with per-hostgroup threshold catalog (longest-prefix-match), dominant-proto vector classification, multi-criteria 0..1 confidence score, and per-(host, vector) IDLE/ACTIVE/COOLDOWN state machine emitting ULID-keyed AttackEvents on a buffered channel.

## What Was Built

### AttackEvent Contract (event.go)

```go
type AttackEvent struct {
    IncidentID string     // ULID — stable across started/updated/ended
    State      State      // "started" | "updated" | "ended"
    HostIP     netip.Addr
    Vector     Vector     // "udp_flood" | "icmp_flood"
    Hostgroup  string
    Pps        uint64     // current 1s pps
    Bps        uint64     // current 1s bps
    PeakPps    uint64
    PeakBps    uint64
    Confidence float64    // 0..1
    StartedAt  time.Time
    EndedAt    time.Time  // zero when not ended
    Now        time.Time
}
```

Helper: `IsP1Vector(v Vector) bool` — true for UDPFlood/ICMPFlood only.

### Catalog API (thresholds.go)

| Symbol | Description |
|--------|-------------|
| `NewCatalogFromThresholds([]Threshold) *Catalog` | Build catalog in-memory (used in tests and from LoadCatalog) |
| `LoadCatalog(ctx, pool) (*Catalog, error)` | Load from Postgres `thresholds JOIN hostgroups` |
| `(*Catalog).Lookup(ip) []Threshold` | Longest-prefix-match; returns all vectors for the best hostgroup |
| `(*Catalog).LookupByVector(ip, v) (Threshold, bool)` | Filter Lookup result to one vector |

Longest-prefix-match is implemented by sorting entries descending by `Prefix.Bits()` at construction time, then scanning until the prefix length decreases past the first match.

### Classify (classify.go)

```go
func Classify(buckets []aggregate.Bucket) Vector
```

- Returns `VectorUDPFlood` if `2*udpPps > totalPps` (UDP strictly > 50%)
- Returns `VectorICMPFlood` if `2*icmpPps > totalPps`
- Returns `""` otherwise (balanced, TCP-dominant, or zero traffic)

### Confidence Score (score.go)

```go
func Confidence(avgPps, avgBps uint64, t Threshold, durSec int) float64
```

- `score = 0.4*sat(ppsRatio) + 0.4*sat(bpsRatio) + 0.2*durFactor`
- `sat(x) = tanh(x-1)` for `x > 1`, else 0 (zero at threshold, saturates toward 1)
- Duration factor only contributes when at least one of pps/bps > threshold
- Clamped to [0, 1]

### StateMachine (state.go)

Constants:
- `CooldownAfterEnd = 60 * time.Second` — D-16: cooldown prevents same (host, vector) re-triggering
- `UpdateAfterDuration = 5 * time.Minute` — D-15: single conditional update threshold

State transitions:
```
IDLE →(DurationViolated >= MinWindowSec)→ ACTIVE: emit StateStarted
ACTIVE →(noViolationTicks >= GraceSec)→ COOLDOWN: emit StateEnded
ACTIVE →(peakDoubled OR duration >= 5min, first time only)→ ACTIVE: emit StateUpdated
COOLDOWN →(now > cooldownUntil)→ IDLE: silent
```

**D-15 confirmation:** `updateEmitted bool` flag ensures at most ONE StateUpdated per incident. Both peak-double (`peakPps > 2*initialPeakPps || peakBps > 2*initialPeakBps`) and time-based (`now - startedAt >= 5min`) triggers set the flag — subsequent violations produce no additional updates.

**D-16 confirmation:** `cooldownUntil = endedAt + 60s`. While `stateActive → stateCooldown`, all Step() calls return nil regardless of violation state. After `now.After(cooldownUntil)`, machine transitions silently to `stateIdle`.

**DETE-01 confirmation:** Engine calls `catalog.Lookup(ip)` first; if empty, returns immediately. No evaluation occurs without an operator-configured threshold. See `TestEngine_NoThreshold_NoDetection`.

### Engine API (engine.go)

```go
func NewEngine(store *aggregate.Store, catalog *Catalog, out chan<- AttackEvent) *Engine
func (e *Engine) Run(ctx context.Context) error   // blocks; returns ctx.Err()
func (e *Engine) Tick(now time.Time)               // exported for deterministic tests
func (e *Engine) Dropped() uint64                  // dropped event counter
```

Per-tick evaluation order:
1. `store.Tick(sec)` — advance sliding window (zero next slot, evict cold hosts)
2. `store.ActiveHosts(sec)` — list hosts with recent traffic
3. For each host: `catalog.Lookup(ip)` → skip if empty → `store.Snapshot(ip, sec, maxWindow)` → `Classify(buckets)` → skip if `""` → `catalog.LookupByVector(ip, vec)` → compute avg pps/bps → update streak counter → `sm.Step(k, in, t)` → non-blocking send

**Tick-latency note (for plan 12):** Add a WARN log if a single tick takes > 500ms. The evaluation loop is O(active_hosts) with O(1) work per host (snapshot + arithmetic), so 1M hosts ≈ 50ms at typical speeds, but the log would surface regressions.

## Test Coverage

| Test | What It Verifies |
|------|-----------------|
| TestCatalog_LongestPrefixMatch | /24 wins over /8 for 192.0.2.5 |
| TestCatalog_NoMatch_ReturnsEmpty | 8.8.8.8 returns empty (no hostgroup) |
| TestCatalog_MultipleVectorsSameHostgroup | Both UDP and ICMP thresholds returned for same /24 |
| TestCatalog_LookupByVector | Filters to single vector; absent vector returns false |
| TestClassify_DominantUDP | 90% UDP pps → VectorUDPFlood |
| TestClassify_DominantICMP | 90% ICMP pps → VectorICMPFlood |
| TestClassify_NoTraffic_ReturnsEmpty | Zero traffic → "" |
| TestClassify_BalancedReturnsEmpty | 50/50 UDP+ICMP → "" |
| TestClassify_OtherDominant_ReturnsEmpty | 80% Other → "" |
| TestConfidence_AtThreshold_Is0 | Exactly at threshold → 0 |
| TestConfidence_DoubleThreshold_PositiveAndBounded | 2× threshold → (0,1] |
| TestConfidence_ClampedTo1 | Extreme traffic → ≤ 1.0 |
| TestSM_IdleToActive_RequiresMinWindow | 4 ticks → no event; 5th → StateStarted |
| TestSM_ActiveToEnded_AfterGrace | grace_sec=3 non-violating ticks → StateEnded |
| TestSM_NonViolatingStreak_ResetByViolation | Violating tick resets streak; needs 3 consecutive after |
| TestSM_UpdateEmitted_OnPeakDouble | pps > 2× initial → StateUpdated; no second update |
| TestSM_UpdateEmitted_AfterFiveMinutes | duration >= 5min → StateUpdated |
| TestSM_Cooldown_PreventsImmediateRetrigger | within 60s cooldown: no StateStarted; after: new one |
| TestSM_IncidentID_StableAcrossEvents | started/updated/ended share same ULID |
| TestEngine_DetectsUDPFlood_EndToEnd | UDP flood above threshold → StateStarted on channel |
| TestEngine_NoThreshold_NoDetection | 8.8.8.8 (no hostgroup) → zero events (DETE-01) |
| TestEngine_ICMPFlood_DetectedByVector | ICMP-dominant traffic → VectorICMPFlood event |
| TestEngine_DroppedCounter_IncrementsOnFullChannel | cap(0) channel → Dropped() > 0 |

**Total: 23 tests. `go test -race ./internal/detect/... -count=1` exits 0.**

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Grace window: tick-count vs time.Sub**
- **Found during:** Task 2 (TestSM_ActiveToEnded_AfterGrace failing)
- **Issue:** Original implementation used `in.Now.Sub(noViolationStart) >= grace` (time-based). At 1Hz, ticks at `t+6s, t+7s, t+8s` give a delta of `t+8s - t+6s = 2s`, not 3s — so `grace_sec=3` never fired on the 3rd tick. The semantic intent is "3 consecutive non-violating ticks" not "3 wall-clock seconds of absence".
- **Fix:** Replaced `noViolationStart time.Time` with `noViolationTicks int`; ENDED fires when `noViolationTicks >= graceTicks`. This matches the 1Hz tick model precisely.
- **Commit:** `5fcd9a1`

**2. [Rule 1 - Bug] Confidence duration factor contributed at exactly-threshold traffic**
- **Found during:** Task 1 (TestConfidence_AtThreshold_Is0 failing, got 0.2 not 0)
- **Issue:** When pps == threshold and bps == threshold, `sat(1.0) = 0` so those terms are 0, but `durFactor = min(durSec/MinWindowSec, 1) = 1.0`, giving score = 0.2.
- **Fix:** Duration factor only applies when `ppsRatio > 1 || bpsRatio > 1`. At exactly the threshold, both ratios are 1.0 so duration does not contribute. The plan's "0 at threshold" expectation is now met.
- **Commit:** `10fa3eb`

**3. [Rule 1 - Bug] Test timing for UpdateAfterDuration**
- **Found during:** Task 2 (TestSM_UpdateEmitted_AfterFiveMinutes failing)
- **Issue:** Test checked at `now + 5min + 1s`. StateStarted fires at tick 5 (now+5s). Elapsed at check = `5min+1s - 5s = 4min56s < 5min`. The 5-minute threshold was not reached.
- **Fix:** Changed test check time to `now + 5min + 6s` so elapsed = `5min+6s - 5s = 5min+1s >= 5min`.
- **Commit:** `5fcd9a1`

## Known Stubs

None — all detect package functionality is fully implemented and tested. The `LoadCatalog` function requires a live `*pgxpool.Pool` (integration test territory); unit tests use `NewCatalogFromThresholds` instead.

## Threat Flags

No new threat surface beyond the plan's threat model. The detect package has no network endpoints, no file I/O, no auth paths. The `out` channel is in-process only.

## Self-Check: PASSED

Files created:
- FOUND: internal/detect/event.go
- FOUND: internal/detect/thresholds.go
- FOUND: internal/detect/classify.go
- FOUND: internal/detect/score.go
- FOUND: internal/detect/state.go
- FOUND: internal/detect/engine.go
- FOUND: internal/detect/thresholds_test.go
- FOUND: internal/detect/classify_test.go
- FOUND: internal/detect/score_test.go
- FOUND: internal/detect/state_test.go
- FOUND: internal/detect/engine_test.go

Commits:
- RED Task 1: `git log --oneline | grep "test(01-07).*Catalog"` → found
- GREEN Task 1: `10fa3eb` — feat(01-07): implement AttackEvent, Catalog, Classify, Confidence
- RED Task 2: `git log --oneline | grep "test(01-07).*stateMachine"` → found
- GREEN Task 2: `5fcd9a1` — feat(01-07): implement stateMachine D-05/D-15/D-16
- RED Task 3: `git log --oneline | grep "test(01-07).*Engine"` → found
- GREEN Task 3: `21cdb75` — feat(01-07): implement Engine 1Hz tick

Verifications:
- `go build ./...` exits 0
- `go vet ./internal/detect/...` exits 0
- `go test -race ./internal/detect/... -count=1` → 23 tests PASS
- DETE-01 guard: TestEngine_NoThreshold_NoDetection → 0 events for unregistered host
- D-15: TestSM_UpdateEmitted_OnPeakDouble → exactly 1 StateUpdated per incident
- D-16: TestSM_Cooldown_PreventsImmediateRetrigger → 60s cooldown enforced
