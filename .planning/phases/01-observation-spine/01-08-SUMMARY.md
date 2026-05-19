---
phase: "01"
plan: "08"
subsystem: incident
tags: [postgres, pgx, incident-store, recorder, attack-event, tdd, pers-01, crash-recovery]
dependency_graph:
  requires: [01-02-postgres-migrations, 01-07-detection-engine]
  provides: [incident-store, incident-recorder, pers-01-persistence]
  affects: [01-10-api, 01-11-dashboard, 01-12-serve-wiring]
tech_stack:
  added: []
  patterns:
    - "pgxpool.Begin + Commit for atomic incident + attack_updates inserts"
    - "GREATEST(peak_pps, $N) in UPDATE for idempotent peak tracking"
    - "fmt.Sprintf for placeholder indices only — values always in args slice (T-01-08-01)"
    - "TDD: RED commit then GREEN for both tasks"
    - "Test gate: os.Getenv('MITIGADOR_TEST_PG_DSN') == '' -> t.Skip()"
key_files:
  created:
    - internal/incident/store.go
    - internal/incident/store_test.go
    - internal/incident/recorder.go
    - internal/incident/recorder_test.go
  modified: []
decisions:
  - "Recorder drain uses default-select to empty channel synchronously after ctx cancel — ensures buffered events flush without blocking if channel is already empty"
  - "CloseOrphans targets created_at < cutoff (not started_at) so incidents inserted but not started in the same 24h window are also recovered"
  - "List.Limit clamped to [1,500], default 50 — matches plan 10 API pagination contract"
  - "ErrNotFound sentinel returned from Get for missing IDs — plan 10 maps this to 404"
metrics:
  duration_seconds: 240
  completed_date: "2026-05-19"
  tasks_completed: 2
  files_created: 4
  files_modified: 0
---

# Phase 1 Plan 8: Incident Store + Recorder Summary

**One-liner:** Typed pgx Store (Create/Update/End/List/Get/CloseOrphans) over the incidents + attack_updates tables, plus a goroutine Recorder that bridges the detect.AttackEvent channel to Postgres — PERS-01 satisfied.

## What Was Built

### Store API (store.go)

| Method | Description |
|--------|-------------|
| `Create(ctx, ev)` | Inserts incidents + attack_updates('started') atomically in one transaction |
| `Update(ctx, ev)` | Bumps `GREATEST(peak_pps, …)` / `GREATEST(peak_bps, …)`, inserts attack_updates('update') |
| `End(ctx, ev)` | Sets `ended_at`, bumps peaks, inserts attack_updates('ended'), all in one transaction |
| `List(ctx, Filter)` | Paginated incidents ordered `started_at DESC`; filters by HostIP, Vector, Since, Until, ActiveOnly |
| `Get(ctx, id)` | Returns Incident + []Update ordered by observed_at; ErrNotFound for missing |
| `ListActive(ctx)` | All open incidents (ended_at IS NULL), limit 500 |
| `CloseOrphans(ctx, cutoff)` | Sets ended_at=cutoff on rows where ended_at IS NULL AND created_at < cutoff; returns row count |

Exported types: `Store`, `NewStore`, `Incident`, `Update`, `Filter`, `ListResult`, `ErrNotFound`.

### Recorder API (recorder.go)

```go
func NewRecorder(store *Store, in <-chan detect.AttackEvent) *Recorder
func (r *Recorder) Run(ctx context.Context) error
```

Dispatch table:
- `StateStarted` → `store.Create`
- `StateUpdated` → `store.Update`
- `StateEnded` → `store.End`
- Unknown state → `slog.Warn` + return (no crash)

Error behaviour: DB errors are logged via `slog.Error` with `incident_id`, `host_ip`, `vector`, `state`, `err`. The `for` loop continues — a single DB failure never halts the recorder.

Drain on cancel: when `ctx` is canceled, `drain(drainCtx)` empties buffered events using a `default:` select arm (non-blocking). The drain context has a 2-second timeout as a hard stop.

### SQL Injection Mitigation (T-01-08-01)

All `fmt.Sprintf` calls in `List()` template only `$%d` placeholder indices (never values). Values are always in the `args []any` slice passed to pgx. Verified:

```
grep 'Sprintf' internal/incident/store.go
→ " AND vector = $%d"
→ " AND started_at >= $%d"
→ " AND started_at < $%d"
→ LIMIT $%d OFFSET $%d
```

Zero value interpolation into SQL strings.

### Crash Recovery (T-01-08-07)

`CloseOrphans(ctx, cutoff)` is called at `mitigador serve` startup (plan 12 wires this with `cutoff = time.Now().Add(-24*time.Hour)`). It logs the count of recovered incidents. Any incident started more recently than 24h stays open so the active detector can re-evaluate normally.

### Multi-tenant Note (T-01-08-02)

Phase 1 is single-tenant per systemd instance (separate Postgres DBs per customer via `mitigador@customer.service`). When MTEN-01 lands in Phase 3, EVERY query in this file must gain `AND tenant_id = $N`. This is the enforced boundary — phase 3 plan checker will verify.

### Retention Note for Plan 12 (PERS-01)

PERS-01 requires ≥ 1-year retention. Plan 12 (serve startup) must register a daily job:

```sql
DELETE FROM incidents WHERE created_at < now() - INTERVAL '12 months';
```

pg_partman partitioning is deferred to Phase 3 per research open question.

### details JSONB Field (T-01-08-06)

`Create` inserts `'{}'::jsonb`. Phase 1 does not store packet metadata. Future use must document what is stored — consider PII/operator-sensitive data before expanding this field.

## Test Coverage

| Test | What It Verifies |
|------|-----------------|
| TestStore_CreateInsertsIncidentAndUpdate | incidents row + attack_updates kind='started' inserted atomically |
| TestStore_Update_BumpsPeaks | GREATEST(peak_pps, …) / GREATEST(peak_bps, …) applied |
| TestStore_End_SetsEndedAt | ended_at populated after End() |
| TestStore_List_FiltersByVector | Vector filter excludes other vectors |
| TestStore_List_FiltersByHostIP | HostIP filter excludes other IPs |
| TestStore_List_FiltersBySinceUntil | Since filter excludes old incidents |
| TestStore_List_PaginatesAndOrdersByStartedAtDESC | Limit=2 returns 2 items; ordered newest-first; Total >= 3 |
| TestStore_Get_ReturnsIncidentAndUpdates | Correct incident + 2 update rows (started + update) |
| TestStore_Get_ReturnsErrNotFoundForMissing | ErrNotFound for nonexistent ID |
| TestStore_ListActive_OnlyOpen | Closed incident excluded; open included |
| TestStore_CloseOrphans_AffectsOldOpenIncidents | Old orphan closed; recent orphan untouched |
| TestStore_Create_RequiresExistingHostgroup_OrNullHostgroupID | Missing hostgroup → hostgroup_id=NULL (subquery) |
| TestRecorder_StartedCallsCreate | StateStarted → row in incidents |
| TestRecorder_EndedCallsEnd | StateEnded → ended_at set |
| TestRecorder_DBErrorDoesNotHaltLoop | DB error logged; next valid event still persisted |
| TestRecorder_DrainsRemainingOnCancel | 3 buffered events all persisted after ctx cancel |

**All 16 tests skip cleanly without `MITIGADOR_TEST_PG_DSN`; pass with live Postgres.**

## Task Commits

| Task | Phase | Hash | Message |
|------|-------|------|---------|
| Task 1 RED | TDD | a84dc82 | test(01-08): add failing tests for Task 1 |
| Task 1 GREEN | TDD | 8190692 | feat(01-08): implement incident.Store |
| Task 2 RED | TDD | 1cef6ce | test(01-08): add failing tests for Task 2 |
| Task 2 GREEN | TDD | f285c4d | feat(01-08): implement incident.Recorder |

## Deviations from Plan

None — plan executed exactly as written. The `drain()` implementation uses a `default:` select arm (not a blocking loop) to prevent hanging when the channel is already empty at cancel time; this is a minor implementation detail within the plan's specified 2s timeout behaviour.

## Known Stubs

None — Store and Recorder are fully implemented. The `details JSONB` field is intentionally `'{}'::jsonb` in Phase 1 (documented in T-01-08-06 above).

## Threat Flags

No new threat surface beyond the plan's threat model. No new network endpoints, auth paths, or file access. All DB access is through the existing pgxpool.

## Notes for Downstream Plans

**Plan 10 (API):** `List()` returns `*ListResult{Items, Total}` — use `Total` for pagination metadata (`X-Total-Count` header or JSON envelope). `Get()` returns `ErrNotFound` → map to HTTP 404.

**Plan 12 (serve wiring):**
1. Call `store.CloseOrphans(ctx, time.Now().Add(-24*time.Hour))` before starting the detector.
2. Log `slog.Info("incident.recovered: closed N orphan incidents")` with the count.
3. Wire `incident.NewRecorder(store, engine.Out())` and launch `rec.Run(ctx)` as a goroutine.
4. Register daily retention job: `DELETE FROM incidents WHERE created_at < now() - INTERVAL '12 months'`.

**Plan 11 (dashboard):** `ListActive()` is the source for the live incident badge count (SSE push).

## Self-Check: PASSED
