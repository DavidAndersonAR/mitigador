---
phase: 01
plan: 03
subsystem: config
tags: [config, viper, validation, env-override, secrets]
dependency_graph:
  requires: [01-01]
  provides: [config-package, config.Load, config.Validate, Config-struct]
  affects: [01-04, 01-05, 01-09, 01-10, 01-12]
tech_stack:
  added:
    - "github.com/spf13/viper v1.21.0 (YAML read + env override, already pinned in 01-01)"
    - "github.com/go-playground/validator/v10 v10.27.0 (struct-tag validation, already pinned in 01-02)"
  patterns:
    - "viper.New() per Load call (no global viper state)"
    - "SetEnvPrefix + SetEnvKeyReplacer + AutomaticEnv for MITIGADOR_<SECTION>_<KEY> convention"
    - "Secret fields redacted in validation error messages via isSecret() field-name check"
    - "validator.WithRequiredStructEnabled() for strict required field checking"
key_files:
  created:
    - internal/config/config.go
    - internal/config/load.go
    - internal/config/validate.go
    - internal/config/load_test.go
    - internal/config/validate_test.go
    - internal/config/testdata/valid.yaml
    - internal/config/testdata/missing_secret.yaml
  modified: []
decisions:
  - "Secret field values redacted in Validate() error messages (T-01-03-05): fields containing Token, Password, Secret, or SessionSecret have value replaced with <redacted>"
  - "viper.New() per Load invocation (not global): tests can run in parallel without global state interference"
  - "os.Stat check before viper.ReadInConfig: allows Load to return a human-readable error mentioning the filename before viper obscures it"
  - "Defaults set for non-required fields: max_conns=16, min_conns=2, receive_buffer_bytes=33554432, log.level=info, log.format=json"
metrics:
  duration_seconds: 160
  completed_date: "2026-05-19"
  tasks_completed: 2
  files_created: 7
  files_modified: 0
---

# Phase 1 Plan 3: Config Loader Summary

**One-liner:** Typed Config struct (7 nested types) with viper-based YAML + env-override loader and validator/v10 struct-tag validation that redacts secret values from error messages.

## What Was Built

### Config Struct (`internal/config/config.go`)

Root struct with 6 top-level sections (Log has no `validate:"required"` — all fields are optional with defaults):

| Field | Type | Validation |
|-------|------|-----------|
| `Postgres` | `Postgres` | required |
| `HTTP` | `HTTP` | required |
| `Ingest` | `Ingest` | required |
| `Telegram` | `Telegram` | required |
| `SMTP` | `SMTP` | required |
| `Log` | `Log` | — (all optional, defaults applied) |

**Nested types:**

- `Postgres`: `DSN` (required, startswith=postgres), `MaxConns` (1-200), `MinConns` (1-200)
- `HTTP`: `ListenAddr` (required, ip), `ListenPort` (1-65535), `SessionSecret` (required, min=32), `AppBaseURL` (required, url)
- `Ingest`: `NetFlow`/`IPFIX`/`SFlow` (each `IngestPort`), `ReceiveBufferBytes` (≥1MiB)
- `IngestPort`: `ListenAddr` (required, ip), `ListenPort` (required, 1-65535)
- `Telegram`: `BotToken` (required, min=20), `AllowedChatIDs` (required, min=1, dive,required)
- `SMTP`: `Host` (hostname|ip), `Port` (1-65535), `Username`, `Password`, `Security` (oneof=starttls tls plain), `FromAddr` (email), `ToAddrs` (min=1, dive,email)
- `Log`: `Level` (oneof=debug info warn error), `Format` (oneof=json text)

### Loader (`internal/config/load.go`)

```go
func Load(path string) (*Config, error)
const DefaultPath = "/etc/mitigador/config.yaml"
```

- `os.Stat` check before viper read → error names the missing file
- `viper.SetEnvPrefix("MITIGADOR")` + `SetEnvKeyReplacer(".", "_")` + `AutomaticEnv()`
- `Validate(&cfg)` called before returning — invalid config fails fast at boot

### Validator (`internal/config/validate.go`)

```go
func Validate(cfg *Config) error
```

- Uses `validator.New(validator.WithRequiredStructEnabled()).Struct(cfg)`
- Returns a multi-line error naming every failing field: `Config.HTTP.SessionSecret: failed "min" (got "<redacted>")`
- Secret fields (containing `Token`, `Password`, `Secret`, `SessionSecret`) have value replaced with `<redacted>` — implements T-01-03-05

### Env Override Convention

| Config key | Env var |
|-----------|---------|
| `postgres.dsn` | `MITIGADOR_POSTGRES_DSN` |
| `postgres.max_conns` | `MITIGADOR_POSTGRES_MAX_CONNS` |
| `http.session_secret` | `MITIGADOR_HTTP_SESSION_SECRET` |
| `http.app_base_url` | `MITIGADOR_HTTP_APP_BASE_URL` |
| `telegram.bot_token` | `MITIGADOR_TELEGRAM_BOT_TOKEN` |
| `smtp.password` | `MITIGADOR_SMTP_PASSWORD` |
| `ingest.receive_buffer_bytes` | `MITIGADOR_INGEST_RECEIVE_BUFFER_BYTES` |

Pattern: `MITIGADOR_<SECTION>_<KEY>` (nested keys use additional underscores per level).

### Defaults Applied by Load()

| Key | Default |
|-----|---------|
| `postgres.max_conns` | `16` |
| `postgres.min_conns` | `2` |
| `ingest.receive_buffer_bytes` | `33554432` (32 MiB) |
| `log.level` | `"info"` |
| `log.format` | `"json"` |

### Tests

9 tests across 2 test files:

| Test | File | Coverage |
|------|------|----------|
| `TestLoad_Valid` | load_test.go | Happy path: all sections populated, ports correct, chat IDs correct |
| `TestLoad_NonExistent` | load_test.go | Missing file: error mentions filename |
| `TestLoad_MissingSecret` | load_test.go | Missing session_secret: error mentions SessionSecret |
| `TestLoad_EnvOverride` | load_test.go | `MITIGADOR_POSTGRES_DSN` overrides YAML value |
| `TestValidate_Valid` | validate_test.go | All-valid config: returns nil |
| `TestValidate_MissingDSN` | validate_test.go | Empty DSN: error mentions "DSN" |
| `TestValidate_ShortSessionSecret` | validate_test.go | 8-char secret: error mentions "min" |
| `TestValidate_EmptyChatIDs` | validate_test.go | nil AllowedChatIDs: error mentions "AllowedChatIDs" |
| `TestValidate_InvalidSecurity` | validate_test.go | Bad SMTP Security enum: error mentions "Security" |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Security] Added secret value redaction to Validate() per threat model T-01-03-05**

- **Found during:** Task 2 implementation (reviewing threat model before coding)
- **Issue:** The plan's Task 2 action code used `fe.Value()` directly in error messages, which would leak `Telegram.BotToken`, `SMTP.Password`, and `HTTP.SessionSecret` values when validation fails
- **Fix:** Added `isSecret()` helper that checks if field name contains `Token`, `Password`, `Secret`, or `SessionSecret` — replaces the value with `<redacted>` in the formatted error string
- **Files modified:** `internal/config/validate.go`
- **Commit:** `3a70066`

The plan's threat model (T-01-03-05) explicitly calls this out as a required mitigation. The plan's action code omitted it. Applied as Rule 2 (missing critical security functionality).

## Known Stubs

None — all exported symbols are fully implemented.

## Threat Flags

No new threat surface introduced. The config package only reads files and env vars; it does not open network connections, write files, or log the Config struct.

T-01-03-01 (downstream plans must not log *Config directly) is documented as a deploy contract for plans 04, 09, 10, 12. Not enforced at runtime in P1.

## Self-Check: PASSED

Files created:
- FOUND: internal/config/config.go
- FOUND: internal/config/load.go
- FOUND: internal/config/validate.go
- FOUND: internal/config/load_test.go
- FOUND: internal/config/validate_test.go
- FOUND: internal/config/testdata/valid.yaml
- FOUND: internal/config/testdata/missing_secret.yaml

Commits:
- FOUND: 3a70066 (feat(01-03): Config struct, Load() viper loader, and Validate() with secret redaction)

Verifications:
- `go build ./...` exits 0
- `go vet ./internal/config/...` exits 0
- `go test ./internal/config/... -count=1` exits 0 (9 tests PASS)
