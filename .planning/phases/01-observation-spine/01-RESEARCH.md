# Phase 1: Observation Spine — Research

**Researched:** 2026-05-18
**Domain:** ISP DDoS observation pipeline (flow ingest → per-host counters → detection → alert + dashboard), greenfield Go service
**Confidence:** HIGH on ingest stack, detection patterns, alert lifecycle, SSE; MEDIUM on exact GoFlow2 v2 embedding ergonomics; HIGH on locked decisions alignment

## Summary

This phase delivers the "observation-only" half of Mitigador: NetFlow v9 / IPFIX / sFlow v5 ingestion via `goflow2/v2` embedded as a library, per-host sliding-window counters in RAM, UDP / ICMP flood detection per /32 with a `started → update → ended` state machine, Telegram + SMTP alerts in pt-BR (with respect to the 30 msg/s + 1 msg/s-per-chat Telegram limits), and an authenticated Vue 3 dashboard fed via SSE — all backed by Postgres for incidents and user / session storage. **No BGP code ships in Phase 1**; this is the bedrock that proves detection works before Phase 2 puts BGP on the network.

The technical risk concentrates in three places: (1) **GoFlow2 v2 embedding** — the v2.2.6 `producer.ProducerInterface` is the right hook, and the recommended wiring is `utils.UDPReceiver → utils.PipeConfig{Producer: customProducer} → in-process channel`. The v3 main branch has reshuffled the API, but the locked decision is v2 so we stick with `github.com/netsampler/goflow2/v2`. (2) **Mikrotik NetFlow v9 byte-order bug** — verified open in FastNetMon issue #985, no upstream fix; the standard workaround is exactly what TELE-04 calls for: ignore the rate the router announces and use a per-exporter override stored in `exporters.sample_rate_override`. (3) **Telegram rate-limit handling** — `go-telegram/bot` provides 429 / `RetryAfter` detection but no built-in queue; we build a small token-bucket worker (30 ops/s global + 1 op/s per chat) in front of every `SendMessage` call.

**Primary recommendation:** Pipeline as Pattern 1 from ARCHITECTURE.md (in-process channels, single binary). One `ReadFrom` goroutine per UDP port → buffered channel → worker pool with a `goflow2/v2/producer` adapter that converts decoded samples into `internal/flow.Record` and routes to a sharded `internal/aggregate.Store`. Detector tick goroutine (1s) walks shards, emits `AttackEvent` to a fan-out broadcaster consumed by alert dispatcher, SSE broker, and incident persister.

## User Constraints (from CONTEXT.md)

### Locked Decisions

#### D-01 — Greenfield Go detector
Detection engine is implemented **greenfield in Go** inside the Mitigador binary. **FastNetMon Community is NOT a runtime dependency** — it is conceptual reference only (same status as `lupael/ddos-protection`). Any text in `PROJECT.md`, `ROADMAP.md`, `STATE.md`, or `.planning/research/STACK.md` that calls FastNetMon "the detection engine" is obsolete after this pivot.

#### D-02 — GoFlow2 v2 embedded as library
Ingest via `netsampler/goflow2/v2`. Covers NetFlow v9 + IPFIX + sFlow v5. No separate binary, no Kafka. Custom producer plugs into Mitigador's internal pipeline.

#### D-03 — UDP listener topology
1 goroutine `ReadFrom` per UDP port (2055 / 4739 / 6343) + buffered channel + worker pool decoding in parallel. SO_REUSEPORT only after a load test shows contention.

#### D-04 — Per-host counters
Ring buffer 60×1s per host IP, sharded by `hash(IP)` across N shards (`N = runtime.NumCPU()` default). Each shard owns its mutex. Global 1s tick advances heads and expires buckets. (TELE-06)

#### D-05 — Detector state machine
Detector emits `AttackEvent` with `started → updates → ended` per key `(host_ip, vector)`. `started` after threshold violated for an anti-flicker minimum window. Periodic `updates` carry peak / avg. `ended` fires after grace period without violation. (DETE-02, 03, 05, 06)

#### D-06 — Exporter gate
TELE-05 validation runs at the ingest gate before any counter update: UDP datagram from an IP outside the `exporters` inventory is dropped, with rate-limited log.

#### D-07..D-10 — Hybrid configuration
- **YAML** at `/etc/mitigador/config.yaml` for infra (Postgres DSN, session secret, HTTP / UDP ports + listen IPs, Telegram bot token + allowed chat IDs, SMTP credentials, `app_base_url`).
- **Postgres tables** for domain truth: `exporters` (with `sample_rate_override` — TELE-04), `hostgroups`, `thresholds`, `alert_channels`, `whitelist` (created in P1, used from P2).
- Seed via CLI: `mitigador config sync` reads optional domain-YAML, upserts idempotently, logs diffs.
- **No hot reload in P1.** Changes require `systemctl restart mitigador`; UDP listeners reopen, counters reset cleanly. Hot reload is Phase 3.
- **No threshold templates in P1.** New hostgroup needs explicit threshold from operator.

#### D-11..D-14 — Authentication
- Table `users` (id, username UNIQUE, password_hash, email, created_at, last_login). CRUD via CLI in P1; UI in Phase 3.
- Bcrypt cost ≥ 12 (`golang.org/x/crypto/bcrypt`).
- Sessions via `alexedwards/scs/v2` + `pgxstore`. Cookies `httpOnly` + `Secure` + `SameSite=Lax`. Sessions survive restart; revocation and audit come free.
- First admin via `mitigador user create <username>` — interactive TTY password prompt, idempotent. No env-var bootstrap, no password in YAML.

#### D-15..D-17 — Alert cadence
- Telegram fires on `started` always, on `ended` always, and on **one** intermediate update only if `peak > 2× initial_peak` **OR** `duration > 5 min` (whichever first). (ALER-02, 06)
- Dedup key `(host_ip, vector)` + 60s grace after `ended` before a new `started` for the same pair is treated as a new incident.
- SMTP follows the same cadence as Telegram (ALER-05).

#### D-18 — DASH-04 in Phase 1
"BGP Sessions" UI component exists but shows an empty-state message *"Nenhuma sessão BGP configurada"*. Phase 2 populates it with real sessions.

### Claude's Discretion

Open for the planner / executor (research below recommends specific options where possible):
- Detailed Postgres schemas for `incidents`, `attack_updates`, `exporters`, `hostgroups`, `thresholds`, `users`, `sessions`, `alert_channels`, `whitelist`.
- Full YAML schema (fields, validation, examples).
- Session timeout policy, CSRF middleware choice, rate limit on the login endpoint.
- Concrete implementation of the Mikrotik byte-order workaround — `exporters.sample_rate_override` semantics free.
- SSE event types, cadence, heartbeat / keepalive interval.
- Telegram rate-limit implementation (token vs leaky bucket; in-memory vs Postgres queue; reconcile after restart).
- Go folder layout (`cmd/` + `internal/`) — anchor to `.planning/research/ARCHITECTURE.md` § Recommended Project Structure, adjust as needed.
- `internal/bgp/` package in P1 — empty / interface stub / skip entirely.
- SPA delivery (Go `embed.FS` vs Nginx vs chi static binding).
- Concrete pt-BR / en-US toggle (vue-i18n in front; alert messages stay pt-BR backend-side and don't depend on the UI toggle).
- Test strategy: synthetic flow generator vs goflow2 fixtures.

### Deferred Ideas (OUT OF SCOPE)

Do NOT plan or research these in Phase 1:
- Carpet-bombing detection (/28 /24 /22) — DETE-04, DETE-07 → Phase 3.
- Full CRUD UI (hostgroups, thresholds, peers, alert channels, whitelist) — DASH-06 → Phase 3.
- Attack timeline + top source ASNs — DASH-07, DASH-10 → Phase 3.
- Hot reload (SIGHUP / LISTEN/NOTIFY) — Phase 3.
- Threshold templates (residencial / corporate / gaming / DNS) — MTEN-03 → Phase 3.
- Baseline mode (DETE-07) — Phase 3.
- Telegram inline-button approve (ALER-03, 04) — Phase 2.
- Multi-tenant systemd instantiated units — MTEN-01..05 → Phase 3.
- Anything BGP (sessions, RTBH, Flowspec, panic, audit log, origin check, TTL) — Phase 2 / 3.
- Populating DASH-04 with real sessions — Phase 2.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| TELE-01 | NetFlow v9 ingest on UDP/2055 with template cache (persistent across restarts) | GoFlow2 v2 `decoders/netflow` handles v9 + IPFIX with template cache keyed by (exporter source IP, source ID, template ID) per RFC 3954 — see § GoFlow2 v2 Embedding |
| TELE-02 | IPFIX ingest on UDP/4739 with template cache | Same code path as v9 in GoFlow2 (`NetFlowPipe`); template cache keyed by observation domain ID (IPFIX equivalent of source ID) |
| TELE-03 | sFlow v5 ingest on UDP/6343 | GoFlow2 `SFlowPipe`; sFlow carries sample rate in-band per sample, no template state to manage |
| TELE-04 | Per-exporter sample-rate override (Mikrotik byte-order workaround) | Verified: FastNetMon issue #985 confirms RouterOS v6.49.6 emits little-endian sampling rate in field 34 → ~16,777,216× inflation. Standard workaround: collector ignores router-announced rate, uses `exporters.sample_rate_override` |
| TELE-05 | Validate exporter source IP against inventory | D-06: gate at UDP ingest before counter update. Drop + rate-limited log if unknown source. See § Exporter Validation Pattern |
| TELE-06 | Per-host counters in 60s sliding window in RAM | D-04: ring buffer 60×1s per IP, sharded by hash(IP); see § Per-Host Sharded Counters |
| TELE-07 | Multi-prefix-length counters (/32, /28, /24, /22) | **Phase 1 covers /32 only.** /28, /24, /22 are carpet-bombing detection — explicitly Phase 3 (DETE-04, DETE-07). CONTEXT.md Phase Boundary confirms this scope reduction |
| DETE-01 | Per-hostgroup thresholds — no globals | Hostgroup = prefix (CIDR) → threshold profile. Lookup at detection tick. Schema in § Postgres Schema |
| DETE-02 | UDP flood per-host detection (pps OR bps over minimum window) | D-05 state machine: pps OR bps threshold violated for `min_window_s` (e.g. 5s) → emit `started` |
| DETE-03 | ICMP flood per-host detection (same logic) | Same code path as DETE-02, different protocol classifier |
| DETE-05 | Confidence score (multi-criteria) | Score = function of (pps_ratio, bps_ratio, duration). Multi-criteria boolean is mandatory; the score is documentation for the operator (see § Detection Scoring) |
| DETE-06 | Vector classification (UDP flood / ICMP flood / etc.) | At `started` time, look at proto distribution of the violating window: dominant proto wins. Carpet-bombing UDP and similar are Phase 3 |
| ALER-01 | Authorized Telegram bot + allowed chat IDs | YAML config: bot token + list of `allowed_chat_ids`. Bot sends only to listed IDs |
| ALER-02 | Alert payload (target IP, vector, pps / bps, duration) | Template in pt-BR. Markdown V2 format. See § Telegram Send Pipeline |
| ALER-05 | Email alert (SMTP) with same content + link to incident | `wneessen/go-mail`. Per-alert email mirrors the Telegram message + URL `${app_base_url}/incidents/<ulid>` |
| ALER-06 | Dedup — no per-tick re-alert; aggregate in window | D-15 + D-16: 1 started + 1 ended always, 1 update conditionally. Dedup key `(host_ip, vector)` + 60s grace |
| ALER-08 | Respect Telegram rate-limit (30 msg/s) and enqueue without dropping | Token-bucket (30/s global + 1/s per chat) in front of `SendMessage`. Handle 429 by reading `RetryAfter` and resuming. See § Telegram Rate-Limit Pattern |
| DASH-01 | Login with server-side session (no JWT) | `alexedwards/scs/v2` + `pgxstore` (D-13). bcrypt cost 12+ |
| DASH-02 | Real-time active attacks via SSE | chi handler + per-client buffered channel + broadcaster (fan-out). See § SSE Architecture |
| DASH-04 | BGP sessions health (P1 stub) | D-18: render empty state, no real data fetch |
| DASH-05 | Exporter health (last flow received per source, arrival rate) | Sidecar in the ingest layer: ring per exporter of recent timestamps; query returns `last_seen`, `pps_recent` |
| DASH-09 | UI in pt-BR (default), toggle en-US | vue-i18n; Naive UI ships with `ptBR` and `enUS` built-in locales (verified) |
| PERS-01 | Incidents persisted in Postgres ≥ 1 year | `incidents` + `attack_updates` tables; partitioning by month with pg_partman, drop oldest at 12 months |
| PERS-03 | Counters in RAM only — not persisted raw | D-04: hot-path counters never touch disk; expired buckets are GC'd |
| PERS-04 | No raw flow records persisted | Hard rule: producer adapter converts → aggregates → discards. No flow archive in P1 (ClickHouse is Phase 4+) |

## Project Constraints (from CLAUDE.md)

CLAUDE.md describes the prescriptive stack and forbidden patterns. Items directly relevant to Phase 1:

- **HTTP router:** chi v5 (`go-chi/chi/v5`). Do NOT use Fiber (fasthttp).
- **Postgres driver:** `jackc/pgx/v5` directly (no `database/sql`).
- **Migrations:** `golang-migrate/migrate/v4`.
- **Telegram:** `go-telegram/bot`. Do NOT use `go-telegram-bot-api/telegram-bot-api` (unmaintained since 2021).
- **SMTP:** `wneessen/go-mail` v0.5+. Do NOT use `go-gomail/gomail` (maintenance mode) or stdlib `net/smtp` alone.
- **Logging:** stdlib `log/slog`. Do NOT use `logrus` or `zap`.
- **ORM:** none — use `sqlc` + `pgx` directly. No GORM.
- **Config:** `spf13/viper`. CLI: `spf13/cobra`.
- **IDs:** `oklog/ulid/v2` for incident IDs.
- **Frontend:** Vue 3.4+, Vite 5.x, TypeScript, Naive UI 2.39+, ECharts 5.5+, pnpm.
- **Frontend package manager:** pnpm (not npm / yarn).
- **No Docker in production:** systemd units only. Docker compose for local dev only.
- **CLAUDE.md says ClickHouse is the flow telemetry store** — this is NOT used in Phase 1 (PERS-04 forbids raw flow persistence; ClickHouse is reserved for Phase 4+). Mitigador in P1 needs only PostgreSQL 16.
- **CLAUDE.md still labels FastNetMon as the engine** in some tables — D-01 supersedes this. Where CONTEXT.md and CLAUDE.md disagree, **CONTEXT.md wins**.

The planner MUST validate generated tasks against this list. Any task that pulls in a forbidden library or skips a required one is a planning bug.

## Standard Stack

### Core (Phase 1 only)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/netsampler/goflow2/v2` | v2.2.6 (Dec 2025) | NetFlow v9 / IPFIX / sFlow v5 decoding | Cloudflare-origin, actively maintained, only complete Go flow decoder ecosystem; v3 in main but unreleased — stick with v2 per D-02 [VERIFIED: pkg.go.dev + go.mod at v2.2.6 tag] |
| `github.com/go-chi/chi/v5` | v5.1+ | HTTP router + middleware | stdlib `net/http`-native, easy SSE flush, idiomatic; CLAUDE.md prescription [CITED: project CLAUDE.md] |
| `github.com/jackc/pgx/v5` | v5.5+ (+ `pgxpool`) | PostgreSQL driver | Faster than `database/sql`, native pool, prepared-statement cache; CLAUDE.md prescription [CITED: project CLAUDE.md] |
| `github.com/golang-migrate/migrate/v4` | v4 latest | Schema migrations w/ embedded files | `iofs` source supports Go 1.16+ `embed.FS` — one binary, migrations applied on boot [VERIFIED: pkg.go.dev, golang-migrate issues #471, #514] |
| `github.com/alexedwards/scs/v2` | v2.9.0 (Jul 2025) | Server-side sessions | LoadAndSave middleware + Postgres store; cookies httpOnly/Secure/SameSite=Lax per D-13 [VERIFIED: GitHub releases] |
| `github.com/alexedwards/scs/pgxstore` | latest | scs store backed by pgx pool | Drops into scs v2; sessions table schema below [VERIFIED: GitHub source] |
| `golang.org/x/crypto/bcrypt` | latest | Password hashing | Cost ≥ 12 per D-12; ~250-300 ms per hash on modern CPU [VERIFIED: SamWhited gist, patrickfav benchmarks] |
| `github.com/go-telegram/bot` | latest (Mar 2026) | Telegram Bot API client | Maintained, Bot API 9.5, exposes `TooManyRequestsError` w/ `RetryAfter` for ALER-08 handling [VERIFIED: GitHub repo] |
| `github.com/wneessen/go-mail` | v0.5+ | SMTP client | STARTTLS / SMTPS, concurrency-safe, modern stdlib-compatible [CITED: project CLAUDE.md] |
| `github.com/spf13/cobra` | latest | CLI subcommands | `mitigador serve / config sync / user create` |
| `github.com/spf13/viper` | latest | YAML / env config | Pairs with cobra; operator-friendly |
| `github.com/oklog/ulid/v2` | v2.1+ | Sortable incident IDs | URL-safe, time-sortable, no UUID-v4 randomness |
| stdlib `log/slog` | Go 1.22+ | Structured logging | CLAUDE.md prescription; no `logrus` / `zap` |

### Supporting / Optional

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/sqlc-dev/sqlc` | v1.27+ | Generate type-safe Go from SQL | Eliminates ORM debate. Strongly recommended; not strictly required for P1 if you prefer raw `pgx` queries |
| `github.com/prometheus/client_golang/prometheus` | latest | `/metrics` exposition | Optional in P1, mandatory in Phase 4. Adding the import in P1 lets us count drops / counters cheaply without retro-fitting |
| `github.com/go-playground/validator/v10` | latest | Struct validation for YAML + API bodies | Helps catch invalid `exporters` rows at config-sync time |
| `github.com/nerdalert/nflow-generator` | latest | NetFlow v5 test generator | Dev / test fixtures only. Note: v5 only — for v9 / IPFIX / sFlow we need our own generator or pcap replay (see § Testing Strategy) [VERIFIED: GitHub] |
| `github.com/sflow/sflowtool` (CLI) | latest | sFlow replay tool | Dev / test only. Can replay pcaps with time compression [VERIFIED: GitHub] |

### Frontend

| Library | Version | Purpose |
|---------|---------|---------|
| Vue 3 | 3.4+ | SPA framework — Composition API + `<script setup>` |
| Vite | 5.x | Dev server + build |
| TypeScript | 5.x | Types |
| `naive-ui` | 2.39+ | Component library — ships with `ptBR` and `enUS` locales out of the box [VERIFIED: tusen-ai/naive-ui src/locales/common contains `ptBR.ts`] |
| `pinia` | latest | State management (current incidents list, user, locale) |
| `vue-router` | latest (v4) | Routing |
| `vue-i18n` | v9+ | UI translation toggle (DASH-09) |
| `@microsoft/fetch-event-source` (or native EventSource) | latest | SSE client w/ Last-Event-ID support |

### Installation

```bash
# Backend
go mod init github.com/<org>/mitigador
go get \
  github.com/netsampler/goflow2/v2@v2.2.6 \
  github.com/go-chi/chi/v5 \
  github.com/jackc/pgx/v5 \
  github.com/jackc/pgx/v5/pgxpool \
  github.com/golang-migrate/migrate/v4 \
  github.com/alexedwards/scs/v2 \
  github.com/alexedwards/scs/pgxstore \
  golang.org/x/crypto/bcrypt \
  github.com/go-telegram/bot \
  github.com/wneessen/go-mail \
  github.com/spf13/cobra \
  github.com/spf13/viper \
  github.com/oklog/ulid/v2

# Dev
go install github.com/air-verse/air@latest

# Frontend
cd web/
pnpm create vite@latest . -- --template vue-ts
pnpm add naive-ui pinia vue-router vue-i18n @microsoft/fetch-event-source
pnpm add -D @types/node vue-tsc
```

**Version verification (run before commit):**

```bash
npm view  # n/a — Go: use pkg.go.dev
go list -m -versions github.com/netsampler/goflow2/v2  # confirm v2.2.6 is the highest v2.x
go list -m -versions github.com/alexedwards/scs/v2     # confirm v2.9.x current
```

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| GoFlow2 v2 embedded | Run `goflow2` binary, consume protobuf via Kafka / stdin | More moving parts, ops burden of Kafka, latency hop. Locked out by D-02 |
| chi v5 | gin / echo | Both work but neither is stdlib-native; chi composes with any `http.Handler` cleanly. Locked by CLAUDE.md |
| scs/v2 + pgxstore | gorilla/sessions, custom JWT | scs has Postgres-backed store + sliding expiry built-in; JWT explicitly rejected by DASH-01 |
| Vue 3 + Naive UI | React + shadcn/ui | Both defensible. Vue + Naive UI locked by CLAUDE.md / STACK.md |
| Native `embed.FS` for SPA | Nginx static + chi API | Single binary deploy is simpler for ISP ops; Nginx adds an unnecessary moving part. Embed wins for Phase 1 |
| pg_partman partitioning for incidents | Plain DELETE WHERE created_at < now() - 12 months | pg_partman handles partition creation + drop atomically; plain DELETE bloats indexes. Recommended even at low volume |

## Architecture Patterns

### Recommended Project Structure

Anchored to `.planning/research/ARCHITECTURE.md` § Recommended Project Structure, simplified for greenfield Go in 2026 (Alex Edwards style: shallow, package-by-feature, no `pkg/`):

```
mitigador/
├── cmd/
│   └── mitigador/             # single binary entrypoint (cobra root)
│       ├── main.go            # invokes cobra
│       ├── serve.go           # `mitigador serve` — boots the daemon
│       ├── config.go          # `mitigador config sync` — DB upsert from YAML
│       └── user.go            # `mitigador user create/list/passwd/delete`
├── internal/
│   ├── config/                # YAML + env loader (viper); validation
│   ├── flow/                  # canonical FlowRecord type (used by ingest + aggregate)
│   ├── ingest/                # UDP listeners + goflow2 producer adapter
│   │   ├── listener.go        # 3 listeners (netflow 2055, ipfix 4739, sflow 6343)
│   │   ├── producer.go        # implements goflow2/v2/producer.ProducerInterface
│   │   ├── exporters.go       # exporter inventory + sample-rate override
│   │   └── health.go          # per-exporter last-seen / pps tracker (DASH-05)
│   ├── aggregate/             # sharded per-host ring-buffer counters
│   │   ├── shard.go           # one shard = locked map[ip]*ring
│   │   ├── ring.go            # 60×1s ring buffer
│   │   └── store.go           # facade: N shards, Tick(), Snapshot(ip)
│   ├── detect/                # tick + threshold eval + state machine
│   │   ├── engine.go          # 1s tick, walks active hosts
│   │   ├── thresholds.go      # hostgroup lookup
│   │   ├── classify.go        # vector classification from proto mix
│   │   ├── score.go           # confidence score from multi-criteria
│   │   └── state.go           # AttackEvent state machine
│   ├── alert/                 # fan-out from AttackEvent → channels
│   │   ├── bus.go             # broadcaster (one input, N consumers)
│   │   ├── telegram/          # rate-limited Telegram sender
│   │   │   ├── sender.go      # token bucket
│   │   │   └── format.go      # pt-BR message templates
│   │   └── email/             # SMTP via wneessen/go-mail
│   ├── api/                   # chi HTTP server
│   │   ├── server.go          # router setup, scs middleware, static SPA
│   │   ├── auth.go            # login / logout handlers
│   │   ├── sse.go             # /events broker + handler
│   │   ├── incidents.go       # list / detail
│   │   ├── exporters.go       # health endpoint
│   │   └── middleware/        # auth required, CSRF, request log
│   ├── incident/              # persistence + bridge from AttackEvent → DB
│   │   ├── store.go           # pgx queries (or sqlc-generated)
│   │   └── recorder.go        # consumes AttackEvent → upsert
│   ├── user/                  # CRUD for users table; bcrypt
│   ├── session/               # scs/v2 manager wiring
│   ├── storage/postgres/      # pool, ping, embed.FS migrations
│   ├── bgp/                   # P1: empty package or single interface stub (D-18)
│   └── version/               # build-info (set by goreleaser later)
├── migrations/                # *.sql, embedded via embed.FS at compile
├── web/                       # Vue 3 + Vite SPA (built into dist/ → embed at build)
│   ├── src/
│   ├── package.json
│   └── vite.config.ts
├── static.go                  # //go:embed web/dist + //go:embed migrations
├── deploy/
│   ├── systemd/mitigador.service
│   └── examples/config.yaml
└── go.mod
```

**Rationale:**
- One `cmd/mitigador` binary — no separate API server in P1. Cobra subcommands cover the operator CLI without splitting binaries.
- `internal/` packages are feature-named (ingest, detect, alert, api), not generic (`util`, `common`). Per Alex Edwards § 11 tips.
- `internal/flow` is the shared record type — both `ingest` (writer) and `aggregate` (consumer) depend on it; keeps the channel signature clean.
- `internal/bgp/` stays as an empty package or an interface stub so Phase 2 has a place to land without restructuring.
- `migrations/` and `web/dist/` are at the repo root because `//go:embed` paths can't escape the package directory; a top-level `static.go` is the conventional landing pad.

### Pattern 1: GoFlow2 v2 Custom Producer → In-Process Channel

**What:** Use GoFlow2's `utils.UDPReceiver` to listen and dispatch raw datagrams to a decoder pipeline, and supply a custom `producer.ProducerInterface` implementation that converts decoded samples into our internal `flow.Record` and writes them to a Go channel — instead of marshalling to protobuf and shipping to Kafka.

**When:** Always, for Phase 1. This is exactly the embedding pattern D-02 calls for.

**Producer interface (v2.2.6) [VERIFIED: pkg.go.dev/github.com/netsampler/goflow2/v2/producer]:**

```go
type ProducerInterface interface {
    // Produce converts a decoded sample message into zero or more flow records.
    // The framework calls this once per decoded NetFlow / IPFIX / sFlow sample.
    Produce(msg interface{}, args *ProduceArgs) ([]ProducerMessage, error)
    Commit([]ProducerMessage)
    Close()
}

type ProduceArgs struct {
    Src            netip.AddrPort   // exporter source UDP socket
    Dst            netip.AddrPort   // local listener
    SamplerAddress netip.Addr       // for sFlow: agent address; for NetFlow: usually Src.Addr
    TimeReceived   time.Time
}

type ProducerMessage interface{}  // we return our own flow.Record
```

**Wiring example (skeleton — verify exact symbols when implementing):**

```go
// internal/ingest/producer.go
package ingest

import (
    "context"
    "net/netip"

    "github.com/netsampler/goflow2/v2/producer"
    "github.com/<org>/mitigador/internal/flow"
)

type ChannelProducer struct {
    out       chan<- flow.Record
    exporters *Inventory // for TELE-04 sample-rate override + TELE-05 validation
}

func (p *ChannelProducer) Produce(msg interface{}, args *producer.ProduceArgs) ([]producer.ProducerMessage, error) {
    // 1. TELE-05 gate: drop unknown exporter sources (rate-limited log).
    exp, ok := p.exporters.Lookup(args.SamplerAddress)
    if !ok {
        p.exporters.LogUnknown(args.SamplerAddress) // rate-limited
        return nil, nil
    }

    // 2. Decode msg into one or more flow.Record values.
    //    msg here is the sample structure from decoders/netflow or decoders/sflow.
    //    Use a type switch to handle both protocol families.
    records := convertToFlowRecords(msg, exp)

    // 3. Apply per-exporter sample-rate override (TELE-04).
    for i := range records {
        if exp.SampleRateOverride > 0 {
            records[i].SampleRate = exp.SampleRateOverride
        }
        // Expand bytes / packets by sample rate so downstream sees real volume.
        records[i].Bytes *= records[i].SampleRate
        records[i].Packets *= records[i].SampleRate
    }

    // 4. Push to the internal channel. Drop-on-full prevents back-pressure stalling the decoder.
    for _, r := range records {
        select {
        case p.out <- r:
        default:
            // Increment drop counter for observability.
        }
    }
    return nil, nil  // we don't propagate downstream Producer messages
}

func (p *ChannelProducer) Commit([]producer.ProducerMessage) {}
func (p *ChannelProducer) Close()                              {}
```

**Listener wiring (one goroutine per port — D-03):**

```go
// internal/ingest/listener.go
package ingest

import (
    "context"
    "github.com/netsampler/goflow2/v2/utils"
    "github.com/netsampler/goflow2/v2/utils/templates"
)

func StartNetFlow(ctx context.Context, addr string, port int, prod producer.ProducerInterface) error {
    recv, err := utils.NewUDPReceiver(&utils.UDPReceiverConfig{
        Sockets: 1,           // D-03: start with 1; tune later via SO_REUSEPORT
        ReceiveBuffer: 32<<20, // SO_RCVBUF = 32 MiB; pair with net.core.rmem_max
        Workers: runtime.NumCPU(),
    })
    if err != nil { return err }

    pipe := utils.NewNetFlowPipe(&utils.PipeConfig{
        Producer:         prod,
        NetFlowTemplater: templates.DefaultTemplateGenerator,
    })

    return recv.Start(addr, port, pipe.DecodeFlow)
}

func StartSFlow(ctx context.Context, addr string, port int, prod producer.ProducerInterface) error {
    recv, _ := utils.NewUDPReceiver(...)
    pipe := utils.NewSFlowPipe(&utils.PipeConfig{Producer: prod})
    return recv.Start(addr, port, pipe.DecodeFlow)
}
```

**Caveats:**
- GoFlow2's API surface shifted between v2.x minors. **Before writing real code, run `go doc github.com/netsampler/goflow2/v2/utils PipeConfig` and `go doc github.com/netsampler/goflow2/v2/producer ProducerInterface` against the installed v2.2.6** to confirm symbol names. The README explicitly invites embedding ("You can build your own collector using this base and replace parts") but the example code lives in `cmd/goflow2/main.go` — read it.
- Some package paths in v2.2.6 use `pkg/goflow2/...` rather than top-level `utils/...`. Confirm with `go doc` once installed.
- The v3 main branch (`module github.com/netsampler/goflow2/v3`) has reshuffled internals; do **not** mix v2 and v3 imports.

[VERIFIED: pkg.go.dev/github.com/netsampler/goflow2/v2/producer for interface signature]
[VERIFIED: github.com/netsampler/goflow2 README "you can build your own collector using this base"]
[ASSUMED: the exact symbol path `utils.NewNetFlowPipe` / `utils.PipeConfig.Producer` — pkg.go.dev evidence is suggestive but read of `cmd/goflow2/main.go` was blocked by the v3 main branch rewrite; planner must confirm against v2.2.6 source on disk]

### Pattern 2: NetFlow v9 / IPFIX Template Cache Keyed Per Exporter

**What:** Template records arrive periodically from each exporter (default Mikrotik refresh ~30 min — too long). Data records arriving before a matching template cannot be decoded. The collector must cache templates keyed by `(exporter_source_ip, source_id_or_observation_domain, template_id)` per RFC 3954.

**Implementation:** GoFlow2 v2 handles this internally via the `utils/templates` package. Default `TemplateSystemGenerator` keys templates correctly. No manual cache code needed — but:

- **Template cache is in-RAM only.** After a Mitigador restart, until each exporter re-sends its templates, data records are silently dropped. **Action:** in `mitigador serve` startup logs, count "data records discarded — no template" per exporter for the first 5 min; if non-zero, surface in DASH-05 ("warming up — waiting for templates from <ip>").
- **Recommend operators set router template refresh interval to 60 s** (CLAUDE.md and ARCHITECTURE.md already note this). Document in Phase 4 router-config snippets.

[VERIFIED: RFC 3954 § 9 (template management); pkg.go.dev confirms goflow2 v2 has `utils/templates` package]

### Pattern 3: Per-Host Sharded Ring-Buffer Counters

**What:** Each host IP keyed into one of N shards by `fnv32(ip) % N`. Shard owns a `map[netip.Addr]*HostRing` and a `sync.Mutex`. `HostRing` is 60 slots of `{pps, bps, proto_breakdown}` indexed by `now_second % 60`. A global 1 Hz ticker advances every shard's "head" pointer and zeros the new head slot (eviction).

**Implementation skeleton:**

```go
// internal/aggregate/shard.go
type Bucket struct {
    Pps      uint64
    Bps      uint64
    PpsUDP   uint64
    PpsICMP  uint64
    PpsOther uint64
    BpsUDP   uint64
    BpsICMP  uint64
}

type HostRing struct {
    Buckets [60]Bucket
    LastSec int64  // last second this host was updated (for cold-eviction)
}

type Shard struct {
    mu    sync.Mutex
    hosts map[netip.Addr]*HostRing
}

type Store struct {
    shards []*Shard
    n      uint32
}

func New(numShards int) *Store {
    s := &Store{n: uint32(numShards), shards: make([]*Shard, numShards)}
    for i := range s.shards {
        s.shards[i] = &Shard{hosts: map[netip.Addr]*HostRing{}}
    }
    return s
}

func (s *Store) shardFor(ip netip.Addr) *Shard {
    h := fnv.New32a()
    h.Write(ip.AsSlice())
    return s.shards[h.Sum32()%s.n]
}

// Update is called per FlowRecord (per packet on the hot path).
func (s *Store) Update(ip netip.Addr, sec int64, r flow.Record) {
    sh := s.shardFor(ip)
    sh.mu.Lock()
    defer sh.mu.Unlock()
    hr, ok := sh.hosts[ip]
    if !ok {
        hr = &HostRing{}
        sh.hosts[ip] = hr
    }
    idx := sec % 60
    hr.LastSec = sec
    b := &hr.Buckets[idx]
    b.Pps += r.Packets
    b.Bps += r.Bytes
    switch r.Proto {
    case flow.ProtoUDP:
        b.PpsUDP += r.Packets; b.BpsUDP += r.Bytes
    case flow.ProtoICMP:
        b.PpsICMP += r.Packets; b.BpsICMP += r.Bytes
    default:
        b.PpsOther += r.Packets
    }
}

// Tick is called once per second by the detector ticker.
// It advances the "current bucket" implicitly by zeroing the new slot for each host.
func (s *Store) Tick(now int64) {
    nextIdx := (now + 1) % 60
    for _, sh := range s.shards {
        sh.mu.Lock()
        for ip, hr := range sh.hosts {
            hr.Buckets[nextIdx] = Bucket{}
            // Cold-eviction: if a host had no traffic for 60 s, drop it.
            if now-hr.LastSec > 60 {
                delete(sh.hosts, ip)
            }
        }
        sh.mu.Unlock()
    }
}

// Snapshot returns the last `window` seconds of a host's data, for the detector.
func (s *Store) Snapshot(ip netip.Addr, now int64, window int) []Bucket {
    sh := s.shardFor(ip)
    sh.mu.Lock()
    defer sh.mu.Unlock()
    hr, ok := sh.hosts[ip]
    if !ok { return nil }
    out := make([]Bucket, window)
    for i := 0; i < window; i++ {
        out[i] = hr.Buckets[(now-int64(i)+60)%60]
    }
    return out
}
```

**Shard count recommendation:**
- `runtime.NumCPU()` is the standard starting point per D-04. Most Go sharded-map references (zutto/shardedmap, kelindar/smutex, Anshu Rai's KV-store article) use NumCPU or a small multiple (2-4×) [VERIFIED: search results].
- A *prime* near NumCPU is a common micro-optimization but **the evidence is weak** for this workload — NumCPU is fine. [ASSUMED] picking a prime materially reduces collisions over NumCPU at typical NumCPU values (8-32); modulo with a non-power-of-two divisor already breaks low-bit collision patterns. Don't over-engineer.
- **Memory budget:** `~1 M hosts × 60 buckets × ~64 B / bucket = ~3.8 GB`. That's the worst-case; in practice an ISP /22 (1024 customers) keeps it at ~64 KB. The eviction in `Tick` keeps the map at "active hosts within last 60 s" which is what matters.

**Lock-free alternatives considered:**
- `sync.Map` — pessimistic for write-heavy workloads; not faster than sharded `map` + `Mutex`.
- Atomic uint64 in fixed-size arrays — would require a fixed mapping IP→slot, doesn't fit a streaming workload.
- Conclusion: **sharded mutex map is the right answer for P1**; revisit only if pprof shows shard contention.

[VERIFIED: dev.to article "Go Concurrent Maps", Medium "Basic of sharding", zutto/shardedmap repo]
[ASSUMED: 3.8 GB worst-case memory estimate — derived arithmetic; sanity-check by running a smoke test]

### Pattern 4: UDP Listener with One Goroutine per Port (D-03)

**Topology:**

```
        ┌──── UDP/2055 ReadFrom goroutine ────┐
        │                                      │
NetFlow │── bytes ──> bufferedDgrams chan ──> worker pool (N workers)
                                                │
                                                ├── decode (goflow2)
                                                │
                                                └── ChannelProducer.Produce → flow.Record chan
        ┌──── UDP/4739 ReadFrom goroutine ────┐
IPFIX   │                                      │
                                                ↓
        ┌──── UDP/6343 ReadFrom goroutine ────┐
sFlow                                           │
                                                ↓
                                       aggregate.Store (sharded)
                                                ↓
                                       detect.Engine (1 Hz tick)
                                                ↓
                                       AttackEvent → alert.Bus + sse.Broker + incident.Recorder
```

GoFlow2's `utils.UDPReceiver` already implements the `ReadFrom + buffered channel + worker pool` pattern — we don't roll our own. Set `Sockets: 1` for P1 (D-03 defers SO_REUSEPORT until measured contention).

**UDP buffer sizing (REQ: avoid kernel-level drops at ISP scale):**

```bash
# /etc/sysctl.d/99-mitigador.conf
net.core.rmem_max     = 67108864    # 64 MiB upper cap
net.core.rmem_default = 33554432    # 32 MiB default
net.core.netdev_max_backlog = 5000  # per-NIC queue depth
```

```go
// In the listener setup, set SO_RCVBUF to 32 MiB. Linux clamps to rmem_max
// if not running CAP_NET_ADMIN; with CAP_NET_ADMIN, SO_RCVBUFFORCE bypasses.
&utils.UDPReceiverConfig{ReceiveBuffer: 32 << 20, ...}
```

**Drop detection:** monitor `nstat | grep UdpRcvbufErrors` or `netstat -su | grep "receive buffer errors"`; expose as a `/metrics` counter for Phase 4 dashboards. In Phase 1 just log a warning every 5 s if drops increased.

[VERIFIED: Red Hat RHEL 10 UDP tuning doc; ESnet fasterdata; Baeldung Linux UDP buffer]

### Pattern 5: SSE Broker with Per-Client Channel + Heartbeat

**What:** chi handler at `/api/events` (auth-required). Each connecting client gets a buffered Go channel; a Broker goroutine fans out every event to every channel. Slow clients are dropped when their channel fills (backpressure).

**Skeleton:**

```go
// internal/api/sse.go
type Event struct {
    ID    string            // ULID — supports Last-Event-ID resume (P1: best-effort; full replay is P3)
    Type  string            // "incident.started" | "incident.updated" | "incident.ended" | "heartbeat"
    Data  any               // marshalled as JSON
}

type Broker struct {
    subscribe   chan chan Event
    unsubscribe chan chan Event
    publish     chan Event
}

func (b *Broker) Run(ctx context.Context) {
    clients := make(map[chan Event]struct{})
    tick := time.NewTicker(15 * time.Second) // heartbeat
    defer tick.Stop()
    for {
        select {
        case c := <-b.subscribe:
            clients[c] = struct{}{}
        case c := <-b.unsubscribe:
            delete(clients, c)
            close(c)
        case ev := <-b.publish:
            for c := range clients {
                select {
                case c <- ev:
                default: // client too slow → drop (do not block fan-out)
                }
            }
        case <-tick.C:
            hb := Event{Type: "heartbeat"}
            for c := range clients {
                select { case c <- hb: default: }
            }
        case <-ctx.Done():
            return
        }
    }
}

func (b *Broker) Handler(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

    flusher, ok := w.(http.Flusher)
    if !ok { http.Error(w, "stream unsupported", 500); return }

    ch := make(chan Event, 16) // per-client buffer; small → fast eviction of slow clients
    b.subscribe <- ch
    defer func() { b.unsubscribe <- ch }()

    // Initial sync: optionally replay from Last-Event-ID (P1 best-effort: just acknowledge).
    if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
        // P1: log it; P3 may replay from incident table.
    }

    for {
        select {
        case ev, ok := <-ch:
            if !ok { return }
            payload, _ := json.Marshal(ev.Data)
            fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, payload)
            flusher.Flush()
        case <-r.Context().Done():
            return
        }
    }
}
```

**Notes:**
- Heartbeat every 15 s prevents proxies (nginx default 60 s) from closing idle connections.
- `X-Accel-Buffering: no` is critical when running behind nginx (which is the prod path even though we embed the SPA).
- Backpressure: small per-client buffer (16) + drop-on-full. Slow client → loses events → reconnects → gets fresh snapshot from the REST endpoint. Don't block the broker.
- Last-Event-ID handling is P1-best-effort; full event replay (which requires a per-event log on disk) is deferred.

[VERIFIED: oneuptime SSE-in-Go guides; thoughtbot Writing SSE Server in Go; Three Dots Labs blog]

### Pattern 6: Telegram Rate-Limited Sender

**Telegram limits [VERIFIED: core.telegram.org/bots/faq]:**
- ~30 messages / second total per bot (bulk broadcast).
- ~1 message / second per chat sustained.
- 20 messages / minute per group chat.
- Exceeding → HTTP 429 with `retry_after` field (seconds to wait).

**Pattern: dual token bucket.**

```go
// internal/alert/telegram/sender.go
type Sender struct {
    bot          *bot.Bot
    global       *rate.Limiter             // 30/s global
    perChat      map[int64]*rate.Limiter   // 1/s per chat
    perChatMu    sync.Mutex
    queue        chan outbound             // buffered, fed by alert.Bus consumer
}

func (s *Sender) limiterFor(chatID int64) *rate.Limiter {
    s.perChatMu.Lock()
    defer s.perChatMu.Unlock()
    lim, ok := s.perChat[chatID]
    if !ok {
        lim = rate.NewLimiter(rate.Every(time.Second), 1) // 1/s burst 1
        s.perChat[chatID] = lim
    }
    return lim
}

func (s *Sender) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case out := <-s.queue:
            // Wait on both limiters before sending.
            s.global.Wait(ctx)
            s.limiterFor(out.ChatID).Wait(ctx)

            _, err := s.bot.SendMessage(ctx, &bot.SendMessageParams{
                ChatID: out.ChatID, Text: out.Text, ParseMode: "MarkdownV2",
            })
            if err != nil {
                // Inspect for 429: bot package wraps as *bot.TooManyRequestsError with RetryAfter.
                if tooMany, ok := err.(*bot.TooManyRequestsError); ok {
                    time.Sleep(time.Duration(tooMany.RetryAfter+1) * time.Second)
                    // Re-enqueue at head — bounded retry, drop after N.
                    s.requeue(out)
                    continue
                }
                slog.Error("telegram send failed", "err", err, "chat", out.ChatID)
            }
        }
    }
}
```

Use `golang.org/x/time/rate` — stdlib-ish token bucket already in many Go projects.

**ALER-08 compliance check:**
- Queue is **bounded** (e.g., 1000). If full, drop **lowest priority** (P1: only one priority — drop oldest with WARNING log; D-17 says SMTP gets same cadence, so a Telegram drop doesn't lose the alert entirely). Never block detection.
- Persistence across restart is **out of P1 scope** — in-memory queue is acceptable per Claude's discretion. Document as known limitation.
- Telegram changed to a dynamic token-bucket in API 7.0 (2025) where exact numbers vary by bot age and history [CITED: gramio.dev/rate-limits, hfeu-telegram.com Bot API Rate Limits 2026]. **Always honor the `retry_after` from 429 over the 30/s rule of thumb.**

[VERIFIED: github.com/go-telegram/bot README — TooManyRequestsError + RetryAfter exposed]
[VERIFIED: core.telegram.org/bots/faq — 30/s broadcast + 1/s per chat]

### Pattern 7: AttackEvent State Machine

**State per `(host_ip, vector)` key:**

```
                  threshold_exceeded
                  for ≥ min_window_s
       IDLE  ─────────────────────────────►  ACTIVE
        ▲                                       │
        │                                       │ every update_interval_s
        │                                       │ check: peak > 2×initial OR duration > 5min
        │                                       │   → emit UPDATE (once per incident, per D-15)
        │                                       │
        │                                       │ no-violation for ≥ grace_s (60s default, D-16)
        │                                       │   → emit ENDED, transition to COOLDOWN
        │                                       ▼
        │                                   COOLDOWN  (60s grace per D-16)
        │                                       │
        └───────────────────────────────────────┘
                  cooldown_done → IDLE
```

Each transition produces an `AttackEvent` published to `alert.Bus`:

```go
type AttackEvent struct {
    IncidentID  string         // ULID, stable across started/update/ended
    State       State          // STARTED | UPDATED | ENDED
    HostIP      netip.Addr
    Vector      Vector         // UDP_FLOOD | ICMP_FLOOD (P1 only — others P3)
    Pps         uint64
    Bps         uint64
    PeakPps     uint64
    PeakBps     uint64
    StartedAt   time.Time
    EndedAt     time.Time      // zero if not ended
    Confidence  float64        // 0..1 — see § Detection Scoring
    Hostgroup   string
}
```

`incident.Recorder` consumes events: on `STARTED` it inserts into `incidents`; on `UPDATED` it inserts into `attack_updates`; on `ENDED` it updates `incidents.ended_at` and inserts a final `attack_updates`.

[VERIFIED: maps to D-05 + D-15 + D-16 + ALER-06]

### Pattern 8: Detection Scoring (DETE-05)

Confidence score combines pps_ratio, bps_ratio, and duration. Simple weighted formula:

```go
// internal/detect/score.go
func confidence(b []aggregate.Bucket, t Thresholds, durSec int) float64 {
    avgPps := avg(b, func(x Bucket) uint64 { return x.Pps })
    avgBps := avg(b, func(x Bucket) uint64 { return x.Bps })
    ppsRatio := float64(avgPps) / float64(t.Pps)   // > 1 = exceeding
    bpsRatio := float64(avgBps) / float64(t.Bps)
    durFactor := math.Min(float64(durSec)/float64(t.SustainSec), 1.0)

    // All three normalized into [0..1] via tanh-style saturation.
    score := 0.4*sat(ppsRatio) + 0.4*sat(bpsRatio) + 0.2*durFactor
    return score
}

func sat(x float64) float64 {
    if x <= 1 { return 0 }
    return math.Tanh(x - 1)  // 0 at threshold, ~0.76 at 2×, ~0.96 at 3×
}
```

P1: surface the score in the alert payload (informational). P2+ may use it to gate auto-mitigation.

[ASSUMED] this exact formula — the requirement (DETE-05) is satisfied by any defensible multi-criteria score; the planner / executor may pick a different blend.

### Anti-Patterns to Avoid

- **Persisting raw flow records.** PERS-04 — non-negotiable. The producer adapter converts and discards.
- **Sharing a single mutex for the global counter map.** Use shards (D-04). One global lock will be the bottleneck at any non-trivial flow rate.
- **Blocking detection on Telegram I/O.** The alert.Bus has its own consumer goroutines; detection emits and forgets.
- **Trusting Mikrotik's announced sample rate.** TELE-04 — always honor `exporters.sample_rate_override` if set.
- **Tight `for` loop polling for new attacks.** Drive everything off a 1 Hz `time.Ticker` in `detect.Engine.Tick`.
- **Sessions in cookies with secret payloads.** scs/v2 stores only the session token in the cookie; the data lives in Postgres. Never serialize user state into the cookie body.
- **CSRF disabled because "we use SameSite=Lax".** SameSite-Lax helps for top-level navigation but NOT for embedded forms. Add a CSRF token check on every non-GET handler. Recommend `gorilla/csrf` or roll a small middleware that compares a token from a session value to a header / form field.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| NetFlow / IPFIX template cache | Custom template state machine | `goflow2/v2/utils/templates` | RFC 3954 § 9 has edge cases (template updates, option templates, ID reuse) that take weeks to get right |
| sFlow datagram parsing | Custom binary decoder | `goflow2/v2/decoders/sflow` | sFlow v5 has 30+ sample / record types; even sflowtool is non-trivial |
| Server-side sessions | Custom cookie + DB lookup | `alexedwards/scs/v2` + pgxstore | Sliding expiry, CSRF integration, store rotation, token renew all handled |
| Password hashing | sha256 / scrypt yourself | `golang.org/x/crypto/bcrypt` | Bcrypt is the right primitive at the right cost; D-12 mandates cost ≥ 12 |
| Telegram client | HTTP wrapper around api.telegram.org | `go-telegram/bot` | 200+ methods, file upload, MarkdownV2 escaping, 429 handling |
| SMTP client | `net/smtp` directly | `wneessen/go-mail` | DKIM, STARTTLS, SMTPS, attachment helpers, concurrency-safe |
| DB migrations | Manual `ALTER TABLE` on boot | `golang-migrate/migrate/v4` w/ `iofs` source | Up/down, dirty-state recovery, versioning |
| Time-partitioned retention | Cron `DELETE WHERE` | `pg_partman` extension | Atomic partition drop, no index bloat, predictable disk reclaim |
| Rate limiter | Custom token bucket | `golang.org/x/time/rate` | Battle-tested, exact rate math, context-aware Wait |
| ULID generation | Custom ID scheme | `oklog/ulid/v2` | Sortable, URL-safe, mistake-proof |
| pt-BR component strings | Translate yourself | Naive UI built-in `ptBR` locale | Verified present in `src/locales/common/ptBR.ts` |
| SSE pattern | Custom heartbeat / fan-out | chi + per-client channel broker (Pattern 5) | Use stdlib `http.Flusher`; broker pattern in § Pattern 5 |
| Go project layout | Decide architecture from scratch | Alex Edwards' shallow layout (`cmd/` + `internal/feature/`) | Avoid `pkg/` / deep nesting; descriptive package names |

**Key insight:** This phase is integration of well-known components, not novel engineering. Almost every line of "interesting code" is in `internal/aggregate` (sharded ring buffers), `internal/detect` (state machine), and `internal/ingest/producer.go` (goflow2 adapter). Everything else should be plumbing.

## Runtime State Inventory

**This is a greenfield phase — no existing runtime state to migrate.** No old strings, no old IDs, no installed packages with stale names.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — repo is empty of code/data | None |
| Live service config | None — no existing systemd unit, no running daemon | None |
| OS-registered state | None | None |
| Secrets / env vars | New: `MITIGADOR_TELEGRAM_BOT_TOKEN`, `MITIGADOR_SMTP_PASSWORD`, `MITIGADOR_SESSION_SECRET`, `MITIGADOR_POSTGRES_PASSWORD` — all introduced fresh in P1; not a migration | Document in `deploy/examples/config.yaml` and `deploy/systemd/mitigador.service` (EnvironmentFile=) |
| Build artifacts | None — first build | None |

## Common Pitfalls

(Cross-references to `.planning/research/PITFALLS.md` where deeper coverage exists.)

### Pitfall 1: Mikrotik NetFlow v9 sample-rate byte-order bug
**What goes wrong:** Mikrotik RouterOS v6.49.6+ encodes the sampling rate (field 34) in little-endian instead of big-endian. Collector receives e.g. 16,777,216 for a configured value of 1 — a ~1000-16M× inflation depending on the value [VERIFIED: FastNetMon issue #985].
**Why it happens:** RouterOS bug; not fixed upstream as of 2026-05.
**How to avoid:** Per-exporter `sample_rate_override` column in the `exporters` table (TELE-04, D-07). When set, the ingest producer **ignores** the router-announced rate and uses the override. Operator runs `mitigador config sync` with the override value confirmed against their physical router config.
**Warning signs:** Detected pps / bps grossly under-counts what router link counters show; OR detection fires constantly because sample rate is interpreted as ~16 M, multiplying flow volume astronomically.
**Test plan:** unit test the producer adapter with a synthetic NetFlow datagram carrying an inflated sample rate; assert override wins.

### Pitfall 2: Template loss after Mitigador restart
**What goes wrong:** Mitigador restarts. Until each exporter re-sends its templates (default Mikrotik / Cisco interval 30 min), data records are silently undecoded and dropped.
**How to avoid:** (a) Document operator-facing recommendation: set router NetFlow template refresh interval to 60 s. (b) Log "data records discarded — no template" per exporter; surface in DASH-05 ("warming up") for first 5 min after boot. (c) **Do not** try to persist templates across restarts in P1 — adds DB writes and a recovery edge-case; cost outweighs the 30 s of cold-start blindness when refresh is 60 s.
**Warning signs:** zero counters from a known-good exporter immediately after restart.

### Pitfall 3: UDP socket receive-buffer overflow
**What goes wrong:** Burst of NetFlow / sFlow arrives faster than the goroutine can drain. Kernel drops the overflow. Detection sees lower-than-real rates → misses attacks.
**How to avoid:** `net.core.rmem_max = 64 MiB`, set `SO_RCVBUF = 32 MiB` on each socket, monitor `nstat | grep UdpRcvbufErrors`. Document in deploy README. [VERIFIED: Red Hat tuning guide; ESnet fasterdata]
**Warning signs:** `netstat -su | grep "receive buffer errors"` shows non-zero and increasing.

### Pitfall 4: Telegram rate-limit storm
**What goes wrong:** Many attacks fire in a short window; sender exceeds 30/s globally or 1/s per chat; 429 returned; messages delayed by `retry_after`; the most important alert arrives late or never.
**How to avoid:** Dual token bucket (global 30/s + per-chat 1/s); bounded in-memory queue; honor 429 `retry_after`; never drop without WARN log. See § Pattern 6.
**Warning signs:** repeated `TooManyRequestsError` in logs; queue size approaching cap.
[VERIFIED: core.telegram.org/bots/faq; gramio.dev/rate-limits]

### Pitfall 5: SSE connection death from idle proxies
**What goes wrong:** nginx (or any intermediate proxy) closes the SSE connection after 60 s of silence. Browser reconnects, but for users behind corporate proxies the cycle repeats.
**How to avoid:** Send a heartbeat event (or `: keep-alive\n\n` SSE comment) every 15 s; set `X-Accel-Buffering: no` header; set proxy `proxy_read_timeout` to 5 min minimum in deploy docs.
**Warning signs:** users report "live view stops updating after a minute."

### Pitfall 6: Bcrypt cost calibration mismatch with hardware
**What goes wrong:** Cost 12 on a 2026-vintage server takes 250-300 ms; on a Raspberry Pi-class collector box could be 2-3 s, making login slow. Operator demands lower cost; we refuse (D-12).
**How to avoid:** `mitigador user create` measures hash time and logs it. If > 1 s, log a WARN. Document minimum hardware (4 modern x86 cores). D-12 floor is 12; we can let operators bump to 13-14 on faster hardware via a config knob.
[VERIFIED: Go bcrypt benchmark (SamWhited gist): cost 12 ≈ 289 ms, cost 13 ≈ 578 ms, cost 14 ≈ 1156 ms on a typical modern CPU]

### Pitfall 7: Carpet-bombing scope-creep into Phase 1
**What goes wrong:** While building per-host counters someone "just adds" /24 aggregation. Phase 1 grows. Detection logic mixes scales. Carpet-bombing tests start to be in scope.
**How to avoid:** TELE-07 in P1 = /32 only. The data model in `internal/aggregate` should be keyed strictly by host IP. Multi-resolution support is Phase 3. **CONTEXT.md Phase Boundary explicitly limits this.**

### Pitfall 8: Vue 3 + Vite + embed.FS dev-vs-prod path mismatch
**What goes wrong:** In dev, Vue runs at `localhost:5173`, API at `localhost:8080`. In prod, both come from `:8080` because the SPA is embedded. Hard-coded URLs break.
**How to avoid:** SPA uses relative URLs (`/api/...`, `/events`). Vite dev config proxies `/api` and `/events` to `localhost:8080`. Go server in prod serves `web/dist/*` from `embed.FS` and 404-falls-back to `index.html` for SPA routing.

### Pitfall 9: Naive UI locale missing or stale
**What goes wrong:** Import wrong locale name (`pt-br` vs `ptBR`) → runtime error.
**How to avoid:** Verified naming: `import { ptBR, dateZhCN, ... } from 'naive-ui'`. Brazilian Portuguese is `ptBR` (camelCase, no hyphen) [VERIFIED: GitHub tusen-ai/naive-ui src/locales/common/ptBR.ts]. Also import `dateZhCN`-style date locale — but for pt-BR use the date-fns or dayjs locale yourself; Naive UI's `dateLocale` prop accepts a `NDateLocale` object.

### Pitfall 10: scs/v2 sessions table not created
**What goes wrong:** `pgxstore` requires manual creation of the `sessions` table — it is **not** auto-created [VERIFIED: scs README pgxstore subdirectory]. Operator runs `mitigador serve`, login fails silently with "relation sessions does not exist."
**How to avoid:** Include the exact schema as the **first migration**:

```sql
-- migrations/0001_create_sessions.up.sql
CREATE TABLE IF NOT EXISTS sessions (
    token   TEXT PRIMARY KEY,
    data    BYTEA NOT NULL,
    expiry  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expiry);
```

## Code Examples

### Exporter validation gate (TELE-05, D-06)

```go
// internal/ingest/exporters.go
package ingest

import (
    "net/netip"
    "sync"
    "time"

    "golang.org/x/time/rate"
)

type Exporter struct {
    SourceIP           netip.Addr
    Type               string  // "netflow" | "ipfix" | "sflow"
    SampleRateOverride uint32  // 0 = use rate from datagram (TELE-04)
    Description        string
}

type Inventory struct {
    mu        sync.RWMutex
    byIP      map[netip.Addr]*Exporter
    unknownLim map[netip.Addr]*rate.Limiter // rate-limit "unknown exporter" logs
}

func (i *Inventory) Lookup(ip netip.Addr) (*Exporter, bool) {
    i.mu.RLock()
    defer i.mu.RUnlock()
    e, ok := i.byIP[ip]
    return e, ok
}

func (i *Inventory) LogUnknown(ip netip.Addr) {
    i.mu.Lock()
    lim, ok := i.unknownLim[ip]
    if !ok {
        lim = rate.NewLimiter(rate.Every(time.Minute), 1) // 1 log/min per offending IP
        i.unknownLim[ip] = lim
    }
    i.mu.Unlock()
    if lim.Allow() {
        slog.Warn("flow from unknown exporter", "src_ip", ip.String())
    }
}
```

### Embedded migrations on boot

```go
// internal/storage/postgres/migrate.go
package postgres

import (
    "embed"

    "github.com/golang-migrate/migrate/v4"
    "github.com/golang-migrate/migrate/v4/database/pgx/v5"
    "github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(dsn string) error {
    src, err := iofs.New(migrationsFS, "migrations")
    if err != nil { return err }
    m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
    if err != nil { return err }
    if err := m.Up(); err != nil && err != migrate.ErrNoChange {
        return err
    }
    return nil
}
```

`mitigador serve` calls `postgres.Migrate(cfg.PostgresDSN)` before any other DB work.
[VERIFIED: pkg.go.dev/github.com/golang-migrate/migrate/v4; issue #471 (embed support)]

### chi + scs/v2 + login flow

```go
// internal/api/server.go
func New(cfg Config, db *pgxpool.Pool) http.Handler {
    sm := scs.New()
    sm.Store = pgxstore.New(db)
    sm.Lifetime = 12 * time.Hour
    sm.IdleTimeout = 1 * time.Hour
    sm.Cookie.HttpOnly = true
    sm.Cookie.Secure   = true  // D-13
    sm.Cookie.SameSite = http.SameSiteLaxMode

    r := chi.NewRouter()
    r.Use(middleware.RealIP, middleware.RequestID, middleware.Recoverer, slogMiddleware)
    r.Use(sm.LoadAndSave)

    // Public.
    r.Post("/api/login", loginHandler(db, sm))
    r.Post("/api/logout", logoutHandler(sm))

    // Auth-required.
    r.Group(func(p chi.Router) {
        p.Use(requireAuth(sm))
        p.Use(csrfMiddleware(sm))            // verify token on non-GET
        p.Get("/api/incidents", listIncidents)
        p.Get("/api/incidents/{id}", getIncident)
        p.Get("/api/exporters", listExporters)
        p.Get("/api/events", sseBroker.Handler) // SSE — runs LoadAndSave once, then streams
    })

    // Static SPA from embed.FS with fallback to index.html.
    r.Handle("/*", spaHandler)
    return r
}
```

### Login handler (bcrypt verify + session put)

```go
func loginHandler(db *pgxpool.Pool, sm *scs.SessionManager) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var body struct{ Username, Password string }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
            http.Error(w, "bad request", 400); return
        }
        var id int64; var hash []byte
        err := db.QueryRow(r.Context(),
            `SELECT id, password_hash FROM users WHERE username=$1`, body.Username,
        ).Scan(&id, &hash)
        if err != nil {
            http.Error(w, "invalid credentials", 401); return
        }
        if bcrypt.CompareHashAndPassword(hash, []byte(body.Password)) != nil {
            http.Error(w, "invalid credentials", 401); return
        }
        if err := sm.RenewToken(r.Context()); err != nil {
            http.Error(w, "session error", 500); return
        }
        sm.Put(r.Context(), "user_id", id)
        _, _ = db.Exec(r.Context(), `UPDATE users SET last_login=now() WHERE id=$1`, id)
        w.WriteHeader(204)
    }
}
```

## State of the Art

| Old Approach | Current Approach | Impact |
|--------------|------------------|--------|
| Run FastNetMon Community as the engine, shell out to a notify script (suggested in older STACK.md) | Greenfield Go detector with goflow2 v2 embedded (D-01) | Single binary, no IPC, no FastNetMon config to learn |
| FastNetMon notify-hook over filesystem | In-process `AttackEvent` over Go channel | Sub-millisecond fan-out; no fork-exec per event |
| go-bindata for embedded SQL migrations | Go 1.16+ `embed.FS` + `migrate/v4` `iofs` source | No build-time tool, no generated `.go` files in repo |
| `go-telegram-bot-api/telegram-bot-api` v5 | `go-telegram/bot` (Mar 2026) | Bot API 9.5, exposes 429 / RetryAfter; old lib unmaintained since 2021 |
| `go-gomail/gomail` for SMTP | `wneessen/go-mail` v0.5+ | DKIM, STARTTLS, concurrency-safe; old lib in maintenance mode |
| Custom session cookies | `alexedwards/scs/v2` + pgxstore | Sliding timeout, store-backed revocation, Postgres-persisted |
| Naive shared `sync.Mutex` map for counters | Sharded mutex map (D-04) | Linear scaling to NumCPU; no global lock contention |

**Deprecated / outdated in our context:**
- **FastNetMon as runtime dep** — superseded by D-01.
- **InfluxDB 1.x for time-series counters** — we don't persist counters at all (PERS-03).
- **Fiber / fasthttp** — incompatible with stdlib SSE, ruled out by CLAUDE.md.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `utils.PipeConfig{Producer: ...}` and `utils.NewNetFlowPipe` are the exact v2.2.6 symbol paths to wire a custom producer | § Pattern 1, § Code Examples | Wiring code will need refactor; the *concept* (implement `producer.ProducerInterface`, plug into a pipeline backed by `utils.UDPReceiver`) is verified — only the specific symbol names need confirmation against installed v2.2.6 source |
| A2 | The detection scoring formula `0.4*sat(ppsR) + 0.4*sat(bpsR) + 0.2*durFactor` is acceptable for DETE-05 | § Pattern 8 | Operator may prefer a different weighting — formula is informational only in P1 (alert payload), not gating mitigation. Cheap to change |
| A3 | Worst-case memory budget for sharded counters at 1 M hosts ≈ 3.8 GB | § Pattern 3 | If the ISP has > 1 M active hosts in a 60 s window (unlikely for small / medium BR ISP), memory could become a problem. Mitigate by adding cold-eviction tuning |
| A4 | Prime-shard-count is **not** worth optimizing for over `runtime.NumCPU()` | § Pattern 3 | If pprof shows shard contention, planner can change the shard count without protocol impact |
| A5 | In-memory Telegram queue (no Postgres persistence) is acceptable for P1 | § Pattern 6 | A Mitigador crash during an active attack would lose pending Telegram messages. SMTP cadence is parity (D-17) so a parallel email goes out — net effect is "one of two channels lost" for that incident, acceptable per Claude's discretion |
| A6 | Naive UI's built-in `ptBR` locale covers all components we use in P1 | § Standard Stack | If a Naive UI component renders an untranslated string, fix is to fork-and-override that one entry — low risk, well-understood escape hatch |
| A7 | A 60 s template warm-up after restart is operationally acceptable | § Pitfall 2 | Operator may prefer template persistence across restarts; deferring this to a future phase is a Claude's discretion call |
| A8 | Postgres alone is sufficient — no ClickHouse in P1 | § Standard Stack | CLAUDE.md still lists ClickHouse as the flow telemetry store, but PERS-04 forbids raw flow persistence in P1; reconciliation: ClickHouse is reserved for Phase 4+ when flow archival becomes a feature |

**For the planner / discuss-phase:** A1 is the highest-risk assumption — the planner SHOULD include an early "spike task" that does `go doc` against installed v2.2.6 and writes a 50-line proof-of-concept that brings one NetFlow datagram from `localhost:2055` all the way to a `flow.Record` on a Go channel **before** the rest of the ingest layer is built out. Once that spike is green, A1 collapses.

## Open Questions

1. **Embedded SPA at build time vs at packaging time**
   - What we know: Go `embed.FS` requires the asset directory to exist at compile time. `web/dist/` is produced by `pnpm build`.
   - What's unclear: should `go build` invoke `pnpm build` (via `go:generate` or `goreleaser` hook), or is `pnpm build` a manual prerequisite documented in CONTRIBUTING?
   - Recommendation: explicit two-step in P1 (`pnpm build && go build`); automated via `goreleaser` pre-build hook in Phase 4. Planner can defer choosing.

2. **pg_partman vs manual DELETE for incident retention**
   - What we know: pg_partman is the standard pattern for time-partitioned retention; PERS-01 requires ≥ 1 year.
   - What's unclear: at the expected volume (10s to 100s of incidents per day per tenant), monthly partitions are overkill. Plain `DELETE WHERE created_at < now() - INTERVAL '12 months'` in a daily cron is enough.
   - Recommendation: **start with a daily cron + plain DELETE**, add pg_partman in Phase 3 if volume justifies. Low-risk to refactor.

3. **CSRF token in scs vs separate `gorilla/csrf`**
   - What we know: scs/v2 doesn't ship a CSRF token helper; gorilla/csrf is well-tested but separate.
   - What's unclear: minimal pattern is to put a random token in the session at login (`sm.Put(r.Context(), "csrf", token)`), echo it via a `/api/csrf` GET endpoint, expect it back in every non-GET as an `X-CSRF-Token` header.
   - Recommendation: ship the minimal pattern in P1; full `gorilla/csrf` integration if anything in Phase 3 needs richer features (form-field tokens, etc.).

4. **One bot for the ISP vs one per tenant**
   - What we know: P1 is single-tenant; Telegram bot token + allowed chat IDs in YAML.
   - What's unclear: when multi-tenant (Phase 3) arrives, do tenants share a bot or each gets one?
   - Recommendation: punt to Phase 3. P1 = one bot per install.

5. **vue-i18n for alert / SSE payload language**
   - What we know: DASH-09 toggles UI between pt-BR and en-US.
   - What's unclear: the user wrote "alerts stay pt-BR, don't depend on UI toggle" in CONTEXT.md. The dashboard message rendering of an incident (e.g., "UDP flood em 192.0.2.1") should it follow the UI toggle, or stay pt-BR?
   - Recommendation: backend stores incident events with structured fields (host_ip, vector, pps, bps); the dashboard renders with vue-i18n based on UI toggle. Telegram / Email messages stay pt-BR backend-rendered (per CONTEXT.md). This keeps the data model language-neutral and lets the UI translate freely.

## Environment Availability

> This is greenfield code; the Phase 1 work happens entirely in the dev environment + a small lab Postgres. No external runtime dependencies block planning. Below is what the planner should confirm at the start of execution.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | All backend code | (operator's machine) | 1.22+ required | None — install before starting |
| pnpm | Frontend build | (operator's machine) | 8+ | Use npm if pnpm refuses (suboptimal) |
| PostgreSQL 16 | All storage | dev: local Postgres or docker compose; prod: ISP's own Postgres | 16.x | Postgres 15 should work (pgx v5, scs/v2, migrate/v4 all support it) |
| `nflow-generator` (test) | Synthetic NetFlow v5 — limited utility | Easily `go install` | latest | Write our own v9 / IPFIX / sFlow generator (preferred — see § Testing Strategy) |
| `sflowtool` (test) | sFlow replay | apt / brew install | latest | Or write our own UDP injector |
| Real Mikrotik router (lab) | End-to-end smoke test | (operator's lab) — STATE.md flags this as a Phase 1 lab item | RouterOS 7.x | CI uses synthetic generator only; manual smoke before Phase 1 close |
| Telegram bot token | Telegram alerts smoke test | Free from @BotFather | — | Mock in tests via `httptest.Server` mimicking Bot API |
| SMTP relay | Email alerts smoke test | Maildev / Mailpit container in dev | — | Local Mailpit for dev; ISP relay in prod |

**Missing dependencies with no fallback:** none — every dep is either trivially installable or has a viable mock.

**Missing dependencies with fallback:** Real Mikrotik router for end-to-end test is the only one needing operator effort; CI / development can proceed entirely on synthetic flows.

## Testing Strategy (synthesizing § Pitfalls + Open Questions)

> This is a Claude's-discretion area per CONTEXT.md; recommendations follow.

**Recommended layered approach:**

1. **Unit tests** — pure Go, no I/O:
   - `internal/aggregate` — fill ring buffers, advance ticks, assert evictions.
   - `internal/detect` — drive the state machine with fake snapshots, assert STARTED / UPDATED / ENDED edges fire at the right time.
   - `internal/alert/telegram` — rate limiter test: feed 100 alerts, verify ≤ 30 sends in any 1s window.
   - `internal/ingest/producer` — feed a hand-crafted `(msg, args)` pair, assert correct `flow.Record` lands on the channel; assert TELE-04 override applied; assert TELE-05 drop on unknown exporter.

2. **Integration tests** with synthetic flow generator — preferred over `nflow-generator` since that's NetFlow v5 only:
   - Write a small `cmd/flowgen/` (test-only) that emits valid NetFlow v9 / IPFIX / sFlow v5 to `localhost:2055/4739/6343`. Steal frame layouts from RFC 3954 / RFC 7011 / sFlow v5 spec; the wire format is straightforward.
   - Spin up Mitigador in a test binary (in-process), point flowgen at it, drive an "attack" pattern (10 kpps to 192.0.2.1 for 30 s), assert an AttackEvent STARTED comes out of the alert bus.

3. **Smoke test against a real Mikrotik in lab** before phase close — manual, scripted in `docs/smoke-test.md`. Validates the Mikrotik byte-order workaround end-to-end.

4. **Anti-patterns to avoid:**
   - Do **not** test against `pavel-odintsov/fastnetmon` as a generator (huge dep, wrong protocol). Use a small synthetic emitter we control.
   - Do **not** rely on pcap replay alone — pcaps lock you into a specific sample rate / proto mix; programmatic generation lets you vary parameters.

[VERIFIED: nflow-generator README confirms v5 only; CESNET/FlowTest covers v9/IPFIX but is a heavyweight C testbed]

## Sources

### Primary (HIGH confidence — official docs, verified releases, source code)

- [GoFlow2 GitHub (netsampler/goflow2)](https://github.com/netsampler/goflow2) — main README, latest release v2.2.6 (Dec 2025)
- [GoFlow2 v2 producer package on pkg.go.dev](https://pkg.go.dev/github.com/netsampler/goflow2/v2/producer) — `ProducerInterface`, `ProduceArgs`, `ProducerMessage`
- [GoFlow2 v2 utils package on pkg.go.dev](https://pkg.go.dev/github.com/netsampler/goflow2/v2/utils) — `UDPReceiver`, `PipeConfig`
- [GoFlow2 v2.2.6 go.mod (raw)](https://raw.githubusercontent.com/netsampler/goflow2/v2.2.6/go.mod) — confirmed module path `github.com/netsampler/goflow2/v2`
- [alexedwards/scs README](https://github.com/alexedwards/scs) — v2.9.0 (Jul 2025), LoadAndSave middleware, store interface
- [alexedwards/scs pgxstore subdirectory](https://github.com/alexedwards/scs/tree/master/pgxstore) — exact `sessions` schema (manual creation required)
- [Telegram Bot API FAQ — Rate Limits](https://core.telegram.org/bots/faq) — 30 msg/s broadcast, 1 msg/s per chat, 20/min in groups, 429 + retry_after
- [go-telegram/bot GitHub](https://github.com/go-telegram/bot) — `TooManyRequestsError.RetryAfter`, supports Bot API 9.5
- [FastNetMon issue #985 — Mikrotik NetFlow v9 byte-order](https://github.com/pavel-odintsov/fastnetmon/issues/985) — v6.49.6 little-endian bug, no upstream fix
- [FastNetMon Mikrotik docs](https://fastnetmon.com/docs-fnm-advanced/mikrotik/) — `netflow_ignore_sampling_rate_from_device` + `netflow_sampling_ratio` workaround
- [RFC 3954 — Cisco NetFlow v9](https://datatracker.ietf.org/doc/html/rfc3954) — template management § 9, source ID + observation domain
- [Naive UI src/locales/common](https://github.com/tusen-ai/naive-ui/tree/main/src/locales/common) — confirms `ptBR.ts` exists alongside `enUS.ts`, etc.
- [golang-migrate v4 issue #471 (embed support)](https://github.com/golang-migrate/migrate/issues/471) — embed.FS via iofs source
- [Red Hat RHEL 10 — Tuning UDP connections](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/10/html/network_troubleshooting_and_performance_tuning/tuning-udp-connections) — net.core.rmem_max guidance for high-volume UDP
- [ESnet fasterdata Linux tuning](https://fasterdata.es.net/host-tuning/linux/) — 10 Gbps buffer guidance (32-64 MiB)
- [RFC 7999 — BLACKHOLE Community](https://www.rfc-editor.org/rfc/rfc7999.html) — Phase 2 reference, included for completeness

### Secondary (MEDIUM confidence — credible blogs, cross-verified)

- [Go bcrypt benchmark (SamWhited gist)](https://gist.github.com/SamWhited/ebe4f5923526c0d9220f1b5b23b56eba) — cost 12 ≈ 289 ms, 13 ≈ 578 ms, 14 ≈ 1156 ms
- [gramio.dev/rate-limits](https://gramio.dev/rate-limits) — Telegram dynamic token-bucket since Bot API 7.0 (2025); retry_after primary signal
- [Telegram Bot API rate limits explained (hfeu)](https://hfeu-telegram.com/news/telegram-bot-api-rate-limits-explained-856782827/) — 2026 confirmation
- [Vincent Bernat — eBPF + SO_REUSEPORT for UDP](https://vincent.bernat.ch/en/blog/2026-reuseport-ebpf-go) — Phase 1 deferred but useful reference
- [oneuptime — How to Stream Events with SSE in Go (Jan 2026)](https://oneuptime.com/blog/post/2026-01-25-server-sent-events-streaming-go/view) — broker / heartbeat pattern
- [thoughtbot — Writing a Server-Sent Events server in Go](https://thoughtbot.com/blog/writing-a-server-sent-events-server-in-go) — flush + cleanup
- [Alex Edwards — 11 tips for structuring Go projects](https://www.alexedwards.net/blog/11-tips-for-structuring-your-go-projects) — flat package layout
- [Crunchy Data — Time partitioning with pg_partman](https://www.crunchydata.com/blog/time-partitioning-and-custom-time-intervals-in-postgres-with-pg_partman)
- [DEV.to — Go Concurrent Maps (sharded)](https://dev.to/aaravjoshi/go-concurrent-maps-from-bottlenecks-to-high-performance-sharded-solutions-that-scale-48bk)
- [Better Programming — Serving Vue SPA from Go](https://betterprogramming.pub/how-to-serve-a-single-page-application-using-go-4b9a38d92987)
- [Plixer — Load-balance NetFlow across collectors](https://www.plixer.com/blog/how-can-i-load-balance-my-netflow-traffic-across-multiple-collectors/) — SO_REUSEPORT caveat for template-stateful protocols

### Project research (cross-referenced)

- `.planning/research/STACK.md` — version table (FastNetMon-as-engine lines obsolete per D-01)
- `.planning/research/ARCHITECTURE.md` § Recommended Project Structure, § Pattern 1, § Pattern 4
- `.planning/research/PITFALLS.md` — Pitfalls 2, 3, 5, 12, 15 directly relevant; deeper coverage of Mikrotik byte-order, alert storm, NetFlow templates
- `.planning/research/FEATURES.md` — P1 must-haves matrix
- `.planning/research/SUMMARY.md` — key findings, esp. Pitfall 5 + Mikrotik bug specifics
- `CLAUDE.md` — stack prescription, "NOT to use" list

### Tertiary (LOW confidence — single source or training-time only)

- [GitHub nerdalert/nflow-generator](https://github.com/nerdalert/nflow-generator) — NetFlow v5 only; useful but limited
- [GitHub sflow/sflowtool](https://github.com/sflow/sflowtool) — replay tool only; we still need a generator for IPFIX

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every library version cross-checked against pkg.go.dev / GitHub releases or project CLAUDE.md
- Architecture patterns: HIGH — patterns 1-8 either come from official docs (goflow2, scs, chi) or are standard Go idioms; the sharded counter is the only novel piece and it follows well-known prior art
- Pitfalls: HIGH on technical pitfalls (Mikrotik byte-order verified via FastNetMon issue + Mikrotik forum); MEDIUM on operational warnings (depends on the operator's actual environment)
- Telegram rate-limit handling: HIGH (Telegram FAQ + multiple 2026 sources)
- Vue / Naive UI / SSE: HIGH on Vue + Naive UI ptBR locale (source verified); MEDIUM on the exact SSE + Vue integration (no single official guide; pattern is well-known)
- GoFlow2 v2 exact embedding symbols: MEDIUM (A1 in Assumptions Log) — interface is verified; the precise wiring function names need a `go doc` confirmation at executor time

**Research date:** 2026-05-18
**Valid until:** 2026-06-17 for stable Go libs; 2026-05-25 for fast-moving items (`go-telegram/bot` API + Telegram rate-limit dynamics, since Telegram changes limits without notice)

---

*Research for Phase 1: Observation Spine — ready for planner.*
