---
phase: 01-observation-spine
plan: 02
subsystem: database
tags: [postgres, pgx, golang-migrate, migrations, connection-pool, embed]

requires:
  - phase: 01-01
    provides: go-module, go.mod with pgx v5 and golang-migrate v4 pinned

provides:
  - postgres.NewPool factory (pgxpool.Pool with clamped limits + Ping health-check)
  - postgres.Migrate (embedded iofs migrations, idempotent)
  - postgres.MigrationsFS (embed.FS for external consumers)
  - 9 up + 9 down migration files embedded in binary
  - Full Phase 1 schema: sessions, users, exporters, hostgroups, thresholds, alert_channels, whitelist, incidents, attack_updates

affects: [01-03, 01-04, 01-05, 01-06, 01-07, 01-08, 01-09, 01-10, 01-11, 01-12]

tech-stack:
  added:
    - "github.com/golang-migrate/migrate/v4/database/pgx/v5 (side-effect import for driver registration)"
    - "github.com/golang-migrate/migrate/v4/source/iofs (embedded migration source)"
    - "embed.FS (Go stdlib, go:embed directive)"
  patterns:
    - "//go:embed migrations/*.sql in package-local embed var"
    - "pgx5:// scheme + dsnWithoutScheme() helper for golang-migrate pgx/v5 driver"
    - "errors.Is(err, migrate.ErrNoChange) treated as success (idempotent Migrate)"
    - "NewPool returns error with pool.Close() guard before returning on Ping failure"
    - "Integration tests gated on MITIGADOR_TEST_PG_DSN env var; unit tests run without Postgres"

key-files:
  created:
    - internal/storage/postgres/pool.go
    - internal/storage/postgres/migrate.go
    - internal/storage/postgres/pool_test.go
    - internal/storage/postgres/migrate_test.go
    - internal/storage/postgres/migrations/0001_create_sessions.up.sql
    - internal/storage/postgres/migrations/0001_create_sessions.down.sql
    - internal/storage/postgres/migrations/0002_create_users.up.sql
    - internal/storage/postgres/migrations/0002_create_users.down.sql
    - internal/storage/postgres/migrations/0003_create_exporters.up.sql
    - internal/storage/postgres/migrations/0003_create_exporters.down.sql
    - internal/storage/postgres/migrations/0004_create_hostgroups.up.sql
    - internal/storage/postgres/migrations/0004_create_hostgroups.down.sql
    - internal/storage/postgres/migrations/0005_create_thresholds.up.sql
    - internal/storage/postgres/migrations/0005_create_thresholds.down.sql
    - internal/storage/postgres/migrations/0006_create_alert_channels.up.sql
    - internal/storage/postgres/migrations/0006_create_alert_channels.down.sql
    - internal/storage/postgres/migrations/0007_create_whitelist.up.sql
    - internal/storage/postgres/migrations/0007_create_whitelist.down.sql
    - internal/storage/postgres/migrations/0008_create_incidents.up.sql
    - internal/storage/postgres/migrations/0008_create_incidents.down.sql
    - internal/storage/postgres/migrations/0009_create_attack_updates.up.sql
    - internal/storage/postgres/migrations/0009_create_attack_updates.down.sql
  modified:
    - go.mod (validator downgrade to v10.27.0 for Go 1.25 compat, go directive updated)
    - go.sum

key-decisions:
  - "Migrations live at internal/storage/postgres/migrations/ (not repo root) so //go:embed path is local to package"
  - "golang-migrate pgx/v5 driver registered as pgx5:// — dsnWithoutScheme() strips postgres:// prefix before prepending pgx5://"
  - "go-playground/validator downgraded v10.30.2 -> v10.27.0 (v10.28+ requires Go 1.25; environment has Go 1.25 but previous plan noted 1.23.4 — resolved by letting go mod tidy set correct directive)"
  - "whitelist table created in Phase 1 but enforced from Phase 2 (SAFE-01); documented in migration comment"
  - "pg_partman partitioning for incidents deferred to Phase 3 per RESEARCH open questions"
  - "Migrations use IF NOT EXISTS / IF EXISTS guards — reapplying is safe, but golang-migrate schema_migrations table is the authoritative source of truth"

patterns-established:
  - "Pool factory pattern: all DB consumers call postgres.NewPool, never pgxpool.New directly"
  - "Boot-time migration: cmd serve calls postgres.Migrate(dsn) before serving requests"
  - "Test gate: os.Getenv('MITIGADOR_TEST_PG_DSN') == '' -> t.Skip() for integration tests"

requirements-completed: [PERS-01, PERS-04]

duration: 18min
completed: "2026-05-19"
---

# Phase 1 Plan 2: Postgres Infrastructure Summary

**pgxpool factory + 9 embedded iofs migrations creating the complete Phase 1 schema (sessions through attack_updates) with reversible down-migrations and integration tests gated on MITIGADOR_TEST_PG_DSN.**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-05-19T00:00:00Z
- **Completed:** 2026-05-19T00:18:00Z
- **Tasks:** 2
- **Files modified:** 24 (22 SQL + 4 Go)

## Accomplishments

- 9 up-migration + 9 down-migration SQL files embedded directly into the binary via `//go:embed migrations/*.sql`
- `postgres.NewPool(ctx, dsn, maxConns, minConns)` — pgxpool with sane clamps and Ping health-check before returning
- `postgres.Migrate(dsn)` — idempotent boot-time migration using golang-migrate iofs + pgx/v5 driver
- Unit tests pass without any Postgres (empty/invalid DSN); integration tests skip cleanly if `MITIGADOR_TEST_PG_DSN` is unset

## Migration Files

| Version | Table | Key Constraints |
|---------|-------|-----------------|
| 0001 | `sessions` | scs/pgxstore canonical schema: token TEXT PK, data BYTEA, expiry TIMESTAMPTZ; expiry index |
| 0002 | `users` | username TEXT UNIQUE, password_hash BYTEA (bcrypt-ready), email optional |
| 0003 | `exporters` | source_ip INET UNIQUE, type CHECK('netflow','ipfix','sflow'), sample_rate_override INT DEFAULT 0 |
| 0004 | `hostgroups` | prefix CIDR, name UNIQUE, GIST index for inet containment queries |
| 0005 | `thresholds` | FK hostgroups CASCADE, vector CHECK('udp_flood','icmp_flood'), pps/bps > 0, UNIQUE(hostgroup_id, vector) |
| 0006 | `alert_channels` | type CHECK('telegram','email'), UNIQUE(type,target) |
| 0007 | `whitelist` | prefix CIDR UNIQUE; created P1, enforced P2/SAFE-01 |
| 0008 | `incidents` | id TEXT PK (ULID), 3 indexes: started_at DESC, host_ip, active partial (ended_at IS NULL) |
| 0009 | `attack_updates` | FK incidents CASCADE, composite index (incident_id, observed_at) |

## Schema Details

### `postgres.NewPool` signature
```go
func NewPool(ctx context.Context, dsn string, maxConns, minConns int32) (*pgxpool.Pool, error)
```

### `postgres.Migrate` signature
```go
func Migrate(dsn string) error
```

### `postgres.MigrationsFS`
```go
//go:embed migrations/*.sql
var MigrationsFS embed.FS
```

## Task Commits

Each task was committed atomically:

1. **Task 1: Write 9 migrations (up + down) covering full Phase 1 schema** - `5ef1d98` (feat)
2. **Task 2: Pool factory + Migrate() with embed.FS + integration test** - `a237da8` (feat)

**Plan metadata:** (to be committed with SUMMARY)

## Files Created/Modified

- `internal/storage/postgres/pool.go` — NewPool factory with clamped maxConns/minConns and Ping guard
- `internal/storage/postgres/migrate.go` — Migrate() with //go:embed iofs source and pgx5:// driver
- `internal/storage/postgres/pool_test.go` — Unit: EmptyDSN, InvalidDSN; Integration: Ping (skipped without DSN)
- `internal/storage/postgres/migrate_test.go` — Integration: Apply + idempotency + 9 table existence check (skipped without DSN)
- `internal/storage/postgres/migrations/*.sql` — 18 SQL files (9 up + 9 down)
- `go.mod` — go-playground/validator downgraded v10.30.2 → v10.27.0; go directive updated to 1.25.0
- `go.sum` — updated checksums

## Decisions Made

1. **Migrations location:** Placed under `internal/storage/postgres/migrations/` (not repo root `migrations/`) so the `//go:embed` path is package-local — no `../` escape needed.
2. **pgx5:// driver scheme:** The golang-migrate pgx/v5 driver registers under `"pgx5"`. The `dsnWithoutScheme()` helper strips `postgres://` or `postgresql://` prefix before prepending `pgx5://`.
3. **whitelist in P1:** Table created now so Phase 2 SAFE-01 can enforce it immediately without a schema migration mid-phase.
4. **Partitioning deferred:** pg_partman partitioning for `incidents` deferred to Phase 3 per RESEARCH open question 2. Simple unpartitioned table is sufficient for P1/P2 incident volumes.

## Integration Test Command

```bash
MITIGADOR_TEST_PG_DSN="postgres://user:pass@localhost:5432/mitigador_test?sslmode=disable" \
  go test -v -run TestMigrate_Apply ./internal/storage/postgres/...
```

Expected output: 9 tables confirmed, idempotency confirmed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Validator dependency requires Go 1.25+ with v10.30.2**
- **Found during:** Task 2 (go build attempt)
- **Issue:** `go-playground/validator/v10 v10.30.2` has `go 1.25.0` directive in its go.mod. Plan 01-01 pinned it at v10.30.2. When building with our module, Go 1.25.0 was auto-set in go.mod, but the environment go binary reports Go 1.25.0 (01-01 SUMMARY said 1.23.4 — that was incorrect; `go version` returned `go1.25.0 linux/amd64`).
- **Fix:** Downgraded validator to `v10.27.0` (highest version with `go 1.20` directive, compatible with any toolchain). `go mod tidy` then correctly set go directive to `1.25.0` matching the actual installed toolchain. Both `go build ./...` and `go test ./internal/storage/postgres/...` exit 0.
- **Files modified:** `go.mod`, `go.sum`
- **Verification:** `go build ./...` exits 0; `go test -v ./internal/storage/postgres/...` shows 2 PASS + 2 SKIP
- **Committed in:** `a237da8` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking — dependency version constraint)
**Impact on plan:** Fix was necessary to compile. No functional change — validator v10.27.0 has the same validation API.

## Issues Encountered

None beyond the validator version conflict documented above.

## User Setup Required

None — no external service configuration required for this plan. Integration tests require a Postgres 16 database pointed to by `MITIGADOR_TEST_PG_DSN`.

## Notes

- **`whitelist` table:** Created in Phase 1 but not yet enforced. Phase 2 plan 02-XX (SAFE-01) will refuse RTBH announcements for any /32 contained in any whitelist prefix.
- **Partitioning:** pg_partman partitioning for `incidents` is deferred to Phase 3. The current unpartitioned table with 3 indexes (`started_at DESC`, `host_ip`, `active` partial) handles P1/P2 incident volumes efficiently.
- **T-01-02-04 (accepted risk):** Migrations need CREATE/ALTER privileges. Phase 4 packaging will document least-privilege Postgres user deploy pattern.
- **PERS-04 enforced:** No table for raw flow records exists. Confirmed by schema review.

## Next Phase Readiness

- Plans 03-12 can call `postgres.NewPool(ctx, dsn, 16, 2)` for DB access
- Plans 04/10/12 call `postgres.Migrate(dsn)` on boot
- Schema is stable: downstream plans write SQL/queries against known column names
- `sessions` table matches scs/pgxstore canonical schema — Plan 01-10 (HTTP/session) can use it directly

---
*Phase: 01-observation-spine*
*Completed: 2026-05-19*

## Self-Check: PASSED

Files created:
- FOUND: internal/storage/postgres/pool.go
- FOUND: internal/storage/postgres/migrate.go
- FOUND: internal/storage/postgres/pool_test.go
- FOUND: internal/storage/postgres/migrate_test.go
- FOUND: internal/storage/postgres/migrations/0001_create_sessions.up.sql
- FOUND: internal/storage/postgres/migrations/0008_create_incidents.up.sql
- FOUND: internal/storage/postgres/migrations/0009_create_attack_updates.up.sql

Commits:
- FOUND: 5ef1d98 (Task 1 — 18 migration files)
- FOUND: a237da8 (Task 2 — pool.go, migrate.go, test files, go.mod)

Verifications:
- `go build ./...` exits 0
- `go vet ./internal/storage/postgres/...` exits 0
- `go test ./internal/storage/postgres/...` exits 0 (unit: PASS, integration: SKIP)
