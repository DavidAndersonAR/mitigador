---
phase: 01
plan: 04
subsystem: user-cli
tags: [user-management, bcrypt, cobra-cli, config-sync, domain-yaml, postgres]
dependency_graph:
  requires: [01-02, 01-03]
  provides: [user.Store, config.Domain, config.Sync, mitigador-user-cli, mitigador-config-sync]
  affects: [01-10, 01-12]
tech_stack:
  added:
    - "golang.org/x/term v0.43.0 (TTY password prompt without echo)"
    - "gopkg.in/yaml.v3 v3.0.1 (domain YAML parsing)"
  patterns:
    - "SELECT-before-upsert (ON CONFLICT DO UPDATE) for add/update/unchanged diff tracking"
    - "bcrypt.GenerateFromPassword with named constant BcryptCost=12 — no magic numbers"
    - "pgconn.PgError code 23505 mapped to ErrAlreadyExists sentinel"
    - "pgx.ErrNoRows mapped to ErrNotFound sentinel"
    - "openStore() helper: Load config → Migrate → NewPool → NewStore (operator CLI pattern)"
    - "Passwords read via term.ReadPassword (no echo), written to no log/stdout surface"
    - "Domain Sync wrapped in a single transaction; Rollback deferred on error"
key_files:
  created:
    - internal/user/user.go
    - internal/user/store.go
    - internal/user/store_test.go
    - cmd/mitigador/user.go
    - cmd/mitigador/config_sync.go
    - internal/config/domain.go
    - internal/config/domain_test.go
    - internal/config/testdata/domain.yaml
  modified:
    - cmd/mitigador/main.go
    - go.mod
    - go.sum
decisions:
  - "SELECT-before-upsert instead of xmax trick: SELECT first gives clean add/update/unchanged counts without relying on Postgres internals (xmax). Both are correct; SELECT-first is more readable and portable."
  - "BcryptCost=12 enforced via named constant — store.go uses BcryptCost everywhere, never the literal 12."
  - "D-10 enforced in code: Sync() has zero threshold seeding logic — thresholds only come from the YAML the operator provides."
  - "P1 no-delete contract: Sync() contains no DELETE FROM statement — absent YAML rows are left alone in DB."
  - "Password minimum length: 12 characters enforced in CLI before bcrypt call (T-01-04-02 heuristic)."
  - "config_sync.go created alongside user.go in Task 2 build to allow cmd/mitigador to compile (Go requires all referenced symbols to exist at compile time)."
metrics:
  duration_seconds: 900
  completed_date: "2026-05-19"
  tasks_completed: 3
  files_created: 8
  files_modified: 3
---

# Phase 1 Plan 4: Operator User CLI + Config Sync Summary

**One-liner:** Full bcrypt user CRUD CLI (`create|list|passwd|delete`) with TTY password prompts and idempotent domain YAML upsert (`config sync`) covering exporters, hostgroups, thresholds, alert_channels, and whitelist.

## What Was Built

### `user.Store` API (`internal/user/store.go`)

```go
func NewStore(pool *pgxpool.Pool) *Store
func (s *Store) Create(ctx, username, email, plaintextPassword string) (*User, error)
func (s *Store) Get(ctx, username string) (*User, error)
func (s *Store) GetByID(ctx, id int64) (*User, error)
func (s *Store) VerifyPassword(ctx, username, plaintextPassword string) (*User, error)
func (s *Store) List(ctx) ([]User, error)
func (s *Store) UpdatePassword(ctx, username, newPlaintextPassword string) error
func (s *Store) Delete(ctx, username string) error
func (s *Store) UpdateLastLogin(ctx, id int64) error

var ErrNotFound      = errors.New("user: not found")
var ErrAlreadyExists = errors.New("user: already exists")

const BcryptCost = 12  // per D-12; all GenerateFromPassword calls use this constant
```

### `config.Domain` API (`internal/config/domain.go`)

```go
type Domain struct {
    Exporters     []ExporterEntry
    Hostgroups    []HostgroupEntry
    Thresholds    []ThresholdEntry
    AlertChannels []AlertChannelEntry
    Whitelist     []WhitelistEntry
}

func LoadDomain(path string) (*Domain, error)
func Sync(ctx, pool *pgxpool.Pool, d *Domain) (*SyncDiff, error)

type SyncDiff struct {
    Exporters, Hostgroups, Thresholds, AlertChannels, Whitelist SyncCounts
}
type SyncCounts struct{ Added, Updated, Unchanged int }
```

### CLI Subcommand Usages

```
mitigador user create <username> [--email <addr>]
  Reads password from TTY (no echo), confirms, bcrypt-hashes, inserts into users.
  Errors if password < 12 chars, if passwords mismatch, or if username already exists.
  Output: created user "alice" (id=1)

mitigador user list
  Prints tab-aligned table: USERNAME  EMAIL  CREATED  LAST_LOGIN

mitigador user passwd <username>
  Reads new password from TTY (same prompt as create), rotates bcrypt hash.
  Output: password updated for "alice"

mitigador user delete <username> [--yes]
  Prompts "Delete user <username>? [y/N]:" unless --yes given.
  Output: deleted user "alice"

mitigador config sync --file <path>
  Loads domain.yaml, runs migrations, upserts all 5 domain tables, prints diff:
    exporters:      added=2 updated=0 unchanged=0
    hostgroups:     added=1 updated=0 unchanged=0
    thresholds:     added=1 updated=0 unchanged=0
    alert_channels: added=2 updated=0 unchanged=0
    whitelist:      added=1 updated=0 unchanged=0
```

### Integration Tests (`internal/user/store_test.go`)

9 tests (skip without `MITIGADOR_TEST_PG_DSN`):

| Test | What it verifies |
|------|-----------------|
| `TestStore_CreateAndGet` | Create returns correct user; Get returns same ID |
| `TestStore_BcryptCostIs12` | Hash in DB starts with `$2a$12$` / `$2b$12$` and bcrypt.Cost() == 12 |
| `TestStore_CreateDuplicate_ReturnsErrAlreadyExists` | Second Create returns `ErrAlreadyExists` |
| `TestStore_GetMissing_ReturnsErrNotFound` | Get("nobody") returns `ErrNotFound` |
| `TestStore_VerifyPassword_Success` | Correct password returns non-zero user |
| `TestStore_VerifyPassword_WrongPassword` | Wrong password returns error wrapping `bcrypt.ErrMismatchedHashAndPassword` |
| `TestStore_UpdatePassword` | Old password fails after rotate; new password succeeds |
| `TestStore_Delete` | Delete removes row; Get returns ErrNotFound; Delete("ghost") returns ErrNotFound |
| `TestStore_List` | 3 users returned, ordered by username ASC |

### Domain Sync Tests (`internal/config/domain_test.go`)

6 tests (DB tests skip without `MITIGADOR_TEST_PG_DSN`):

| Test | What it verifies |
|------|-----------------|
| `TestLoadDomain` | Parses testdata/domain.yaml, asserts counts (runs without DSN) |
| `TestLoadDomain_MissingFile` | Returns error for nonexistent path |
| `TestSync_Fresh` | All counts Added on empty DB |
| `TestSync_Idempotent` | Second sync with same Domain: all Unchanged |
| `TestSync_Updated` | Mutated exporter description: Updated=1, Unchanged=1 |
| `TestSync_NoDeleteOnAbsence` | Sync with 1 of 2 exporters: both remain in DB (P1 no-delete) |
| `TestSync_NilDomain` | Returns error immediately |

## Confirmations

**D-10 (no default thresholds seeded):** `Sync()` in `internal/config/domain.go` contains zero threshold seeding logic. Thresholds are only inserted when the operator's YAML includes `thresholds:` entries. Verified: `grep -c "default_threshold\|seed.*threshold" internal/config/domain.go` → 0.

**D-12 (bcrypt cost = 12):** `internal/user/user.go` declares `const BcryptCost = 12`. All `bcrypt.GenerateFromPassword` calls in `store.go` pass `BcryptCost` — no literal `12` used anywhere. The `TestStore_BcryptCostIs12` test asserts both the hash prefix and `bcrypt.Cost()` return value.

**P1 no-delete contract:** `internal/config/domain.go` contains no `DELETE FROM` statement. `TestSync_NoDeleteOnAbsence` verifies a partial sync leaves absent rows untouched.

**T-01-04-01 (password never on stdout/log):** `cmd/mitigador/user.go` reads password into `p1`/`p2` local vars via `term.ReadPassword`. Neither variable appears in any `fmt.Print*` call. Passwords are passed directly to `store.Create()` / `store.UpdatePassword()` which hash them immediately — the plaintext is never stored or logged.

## Deviations from Plan

**1. [Rule 3 - Blocking] config_sync.go created early to unblock build**

- **Found during:** Task 2 — after updating `main.go` to call `newConfigCmd(&configPath)`, the build failed because `newConfigCmd` was not defined (Go requires all referenced symbols at compile time)
- **Fix:** Created `cmd/mitigador/config_sync.go` with the full implementation alongside Task 2, committed both in the Task 2 commit. Task 3 then added the domain package it depends on (`internal/config/domain.go`). The logical separation (Task 2 = user CLI, Task 3 = domain sync) is preserved in the commit history even though `config_sync.go` was physically created in Task 2's commit.
- **Files:** `cmd/mitigador/config_sync.go`
- **Commit:** `4a2eca2`

**2. [Rule 1 - Deviation] SELECT-before-upsert instead of xmax RETURNING trick**

- **Found during:** Task 3 implementation — the plan offered xmax as an option but noted "if RETURNING is awkward, fall back to SELECT-first"
- **Fix:** Used SELECT before ON CONFLICT upsert for clean add/update/unchanged diff counting. Both approaches are correct; SELECT-first avoids Postgres internal `xmax` semantics and is more readable
- **Files:** `internal/config/domain.go`

## Known Stubs

None — all exported symbols are fully implemented with real logic.

## Threat Flags

No new threat surface beyond what the plan's threat model documents. All five domain table upserts use parameterized queries with explicit INET/CIDR casts — SQL injection via malicious YAML values is rejected at the DB level (T-01-04-03 satisfied).

## Self-Check: PASSED

Files created:
- FOUND: internal/user/user.go
- FOUND: internal/user/store.go
- FOUND: internal/user/store_test.go
- FOUND: cmd/mitigador/user.go
- FOUND: cmd/mitigador/config_sync.go
- FOUND: internal/config/domain.go
- FOUND: internal/config/domain_test.go
- FOUND: internal/config/testdata/domain.yaml

Commits:
- FOUND: 87f11c0 feat(01-04): user.Store with bcrypt cost 12 and integration tests
- FOUND: 4a2eca2 feat(01-04): mitigador user create|list|passwd|delete cobra wiring
- FOUND: dc9b800 feat(01-04): config sync + domain YAML upsert

Verifications:
- `go build ./...` exits 0
- `go vet ./...` exits 0
- `go test ./internal/user/... -count=1` exits 0 (skips without DSN)
- `go test ./internal/config/... -count=1` exits 0 (TestLoadDomain passes; sync tests skip without DSN)
- `mitigador user --help` lists create, list, passwd, delete
- `mitigador config sync --help` shows --file flag
- No `fmt.Print` of password variables in cmd/mitigador/user.go
- No `DELETE FROM` in internal/config/domain.go
- `ON CONFLICT (source_ip)`, `ON CONFLICT (name)`, `ON CONFLICT (hostgroup_id, vector)` all present
