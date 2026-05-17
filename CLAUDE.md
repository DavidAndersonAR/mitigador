<!-- GSD:project-start source:PROJECT.md -->
## Project

**Mitigador**

Plataforma de detecção e mitigação automática de ataques DDoS volumétricos voltada para ISPs (provedores de internet) e clientes corporativos. Coleta telemetria de tráfego dos roteadores de borda (sFlow / NetFlow / IPFIX), identifica ataques que tipicamente passam despercebidos pelo monitoramento tradicional e responde automaticamente anunciando rotas de mitigação via BGP (RTBH e Flowspec) — tudo com alertas em tempo real por Telegram, e-mail e dashboard web.

**Core Value:** **Tornar visível e mitigar automaticamente o ataque volumétrico que hoje só é descoberto quando o link já caiu.**
Se tudo mais falhar, o operador precisa receber o alerta e ver a rota de blackhole/flowspec sendo anunciada antes do cliente reclamar.

### Constraints

- **Tech stack**: **Go 1.22+** orquestrador + **FastNetMon Community** engine de detecção + **GoBGP v4.5+** speaker BGP embeddado + **Postgres 16** + **Vue 3 + Vite + Naive UI** + SSE para real-time + **systemd** deploy. Sem Docker em produção (operadores ISP preferem `.deb`/`.rpm`).
- **Timeline**: MVP sólido em **algumas semanas** — não dias (precisa ser confiável) nem meses (próximo ataque pode vir a qualquer momento).
- **Dependencies**: Roteadores precisam (a) suportar export NetFlow v9 ou IPFIX (sFlow opcional, Juniper/Cisco apenas) e (b) aceitar sessão BGP de mitigação dedicada. Sem isso, o produto não funciona.
- **Performance**: Latência sample→mitigação ≤ 7s. Deve ingerir flows sem perder pacotes em ISP de 1–10 Gbps. Hot path (counters últimos 60s) em RAM; cold path (incidentes) em Postgres.
- **Operacional**: Falsos positivos custam reputação — blackholar cliente legítimo é pior que perder ataque pequeno. **Dry-run e manual-approve são defaults**, auto-mitigation só é habilitado após confiança operacional.
- **BGP safety**: Sessão BGP de mitigação **dedicada** (separada da sessão de produção), com filtro saída estrito strippando community blackhole nas outras, origin check (refusar /32 fora do espaço próprio) e TTL obrigatório em toda announcement.
<!-- GSD:project-end -->

<!-- GSD:stack-start source:research/STACK.md -->
## Technology Stack

## TL;DR — Prescriptive Stack
| Layer | Choice | Why |
|---|---|---|
| Flow collector (core engine) | **FastNetMon Community 1.2.8** (Apache 2.0) | Only mature OSS that detects DDoS (not just collects flows). Built-in sFlow/NetFlow v5/v9/IPFIX + threshold detection + per-host counters + ExaBGP/GoBGP hooks. Skip if you want pure custom logic |
| BGP speaker (mitigation) | **GoBGP v4.5+** (Apache 2.0) | gRPC API, modern Go codebase, IPv4/IPv6 Flowspec (RFC 5575) + RTBH, library-mode embedding into Go service |
| Backend language | **Go 1.22+** | Aligns with GoBGP, GoFlow2, and the whole ISP-tooling ecosystem. Single static binary deploys. Concurrency model fits flow ingestion |
| HTTP/API framework | **chi v5** (BSD-3) | Idiomatic `net/http`, zero magic, easy SSE/middleware for an ops dashboard. Don't pick Fiber (fasthttp incompat with stdlib) |
| Flow telemetry storage | **ClickHouse 24.x** | Industry standard for flows (Cloudflare, Akvorado, FastNetMon Advanced all use it). 15-30x compression, 4M rows/sec ingest |
| Incidents/config/users | **PostgreSQL 16** | Boring, correct, transactional. Mitigation history, threshold configs, tenant data — none of that needs a TSDB |
| Real-time push to UI | **Server-Sent Events (SSE)** | One-way server→client is exactly what an ops dashboard needs. Works through corporate proxies/firewalls, simpler than WebSocket, auto-reconnect |
| Frontend | **Vue 3 + Vite + TypeScript + Naive UI** | Lower onboarding cost for a solo/small-team operator project. Composition API + Pinia covers state. React is also defensible if you already know it |
| Charts | **Apache ECharts** (or uPlot for sparklines) | ECharts is the de facto choice for network/ops dashboards; uPlot is the fastest renderer when plotting 10k+ points |
| Telegram alerts | **go-telegram/bot** | Zero-deps, supports Bot API 9.5 (Mar 2026), active, idiomatic Go |
| Email alerts | **wneessen/go-mail v0.5+** | Modern fork of stdlib `net/smtp`, concurrency-safe, supports DKIM/STARTTLS/SMTPS, low dep footprint |
| Process supervision | **systemd units** (one per binary) | Single-binary Go + systemd = ISP-friendly. No Docker daemon to babysit on a coletor box. Provide `docker compose` profile for lab/eval only |
| Build/dist | **goreleaser** + `.deb`/`.rpm` packages | ISPs run Debian/Ubuntu/Rocky/Alma. `apt install mitigador` is what an operator expects |
## Critical Finding: `lupael/ddos-protection` does NOT exist
## Core Technologies
| Technology | Version | Purpose | Why Recommended | Confidence |
|---|---|---|---|---|
| **FastNetMon Community** | 1.2.8 "Hong Kong" (Dec 2024) | DDoS detection engine: ingests sFlow/NetFlow/IPFIX, computes per-host pps/bps, fires thresholds, calls notify script | Only mature open-source DDoS-specific detector. C++ for speed. Apache 2.0. 3.7k★ on GitHub. Used in production by ISPs (Pentanet case study). Community lacks Flowspec — must pair with GoBGP for Flowspec rules | HIGH |
| **GoBGP** | v4.5.0 (Apr 2024)* | BGP speaker — announces RTBH /32 and Flowspec rules to ISP's mitigation peer | Go-native, gRPC API, embeddable as library, supports RFC 5575 IPv4/IPv6 Flowspec, RFC 7674, draft-flowspec-l2vpn, draft-flowspec-v6. Active project (osrg/gobgp). Modern codebase | HIGH |
| **GoFlow2** | v2.2.6 (Dec 2025) | Optional: parallel flow ingestion pipeline for custom analytics, IPFIX/NetFlow v5/v9/sFlow v5 → protobuf/JSON → Kafka or direct ClickHouse | Use when FastNetMon's per-host model is insufficient or for raw flow archive. Cloudflare-origin codebase, actively maintained, used in Akvorado-style pipelines | HIGH |
| **Go** | 1.22+ (1.23 if available) | Backend orchestration layer: REST/SSE API, BGP automation, alert fan-out, multi-tenant config | Ecosystem alignment (GoBGP, GoFlow2). Single static binary = ISP-deployable. Goroutines for parallel ingestion/alerting | HIGH |
| **chi** | v5.1+ | HTTP router + middleware for REST API and SSE endpoints | Built on stdlib `net/http` (no incompatibilities), composable middleware, perfect for SSE handlers. Zero magic, easy to reason about for an ops tool | HIGH |
| **ClickHouse** | 24.8 LTS or 24.x latest | Flow telemetry + attack metrics long-term storage | 15-30x compression on flow data, 4M rows/sec ingest, columnar = fast aggregation queries. Standard choice for flow data (Akvorado, FastNetMon Advanced, ntopng). MergeTree with TTL handles retention | HIGH |
| **PostgreSQL** | 16.x | Incidents, mitigations history, threshold configs, tenants, users, BGP peer config | Boring + correct. Relational data with foreign keys, ACID for "we just announced a blackhole" audit trail. Don't reach for a TSDB here | HIGH |
| **Vue 3** | 3.4+ | Real-time dashboard SPA | Composition API + `<script setup>` + Vite = fast iteration for a small team. Lower hiring/onboarding bar than React for a solo or duo project. Defensible against React if your team already does React | MEDIUM (could be React) |
| **Vite** | 5.x | Dev server + build for Vue | Sub-second HMR, ES-modules native, the only sane choice in 2026 | HIGH |
| **Naive UI** | 2.39+ | Component library for Vue 3 | TypeScript-first, comprehensive (tables, modals, forms, theming), no opinionated layout shell to fight. Alternative: Element Plus (more enterprise feel) | MEDIUM |
| **Apache ECharts** | 5.5+ | Network attack visualization, time-series, sankey for top-talkers | The de facto charting lib for network/ops dashboards. MIT. Handles 10k+ data points without choking | HIGH |
| **go-telegram/bot** | latest (Mar 2026) | Telegram alerts | Zero-deps, Bot API 9.5, idiomatic Go, listed on official Telegram libs page. Avoid the abandoned `go-telegram-bot-api/telegram-bot-api` (last update 2021) | HIGH |
| **wneessen/go-mail** | v0.5+ | SMTP email alerts | Modern Go SMTP with DKIM/STARTTLS, concurrency-safe, stdlib-style. Used over `go-gomail/gomail` which is in maintenance mode | HIGH |
## Supporting Libraries
| Library | Version | Purpose | When to Use |
|---|---|---|---|
| `jackc/pgx` v5 | v5.5+ | PostgreSQL driver | Use directly, not via `database/sql`. Pgx v5 has connection pooling, prepared statement caching, and is faster than the stdlib path |
| `ClickHouse/clickhouse-go` v2 | v2.27+ | ClickHouse driver | Native protocol (binary), batch inserts essential for flow volume |
| `spf13/viper` | latest | Config (YAML + env override) | Standard for Go services with operator-friendly config |
| `spf13/cobra` | latest | CLI commands (`mitigador serve`, `mitigador peer test`, etc.) | Operators expect CLI subcommands; pairs with viper |
| `prometheus/client_golang` | latest | Internal metrics export | Operators run Prometheus; exposing `/metrics` is table stakes |
| `slog` (stdlib) | Go 1.21+ | Structured logging | Use stdlib `log/slog`, not zap/logrus. Stable since 1.21 |
| `golang-migrate/migrate` | v4 | DB migrations for Postgres and ClickHouse | Both DBs supported, simple SQL files |
| `oklog/ulid` v2 | v2.1+ | Incident/mitigation IDs | Sortable, URL-safe, no UUID-v4 randomness pain in logs |
| `osrg/gobgp/v3` (Go module) | latest | Embed GoBGP as library | Avoid out-of-process gRPC dance if a single binary is acceptable. Set `RouteServer` mode for our use |
| `netsampler/goflow2/v2` (Go module) | v2.2+ | Embed flow ingestion in Go | When/if going Option 2 (no FastNetMon) |
| `gorilla/sessions` or `alexedwards/scs` | latest | Web session management | scs/v2 preferred — simpler, supports Postgres store |
| `go-jose/go-jose` v4 | v4+ | JWT for API auth (if multi-user) | Only if multi-user UI; otherwise skip and ship API-key auth |
## Development Tools
| Tool | Purpose | Notes |
|---|---|---|
| `goreleaser` | Build deb/rpm/tar.gz + checksums + GitHub release | Configure to produce `.deb` and `.rpm` — what ISP ops install |
| `air` or `gow` | Live-reload during Go dev | `air` is more popular; configure to also rebuild proto files |
| `sqlc` | Generate type-safe Go from SQL | Eliminates ORM debate; great with Postgres + ClickHouse |
| `mockery` v2 | Generate mocks for tests | For interfaces around BGP speaker + alert sinks |
| `golangci-lint` | Static analysis | Enable `errcheck`, `gosec`, `revive` minimum |
| `pnpm` | Frontend package manager | Faster + disk-efficient vs npm/yarn |
| `playwright` | E2E tests of dashboard | Lighter than Cypress, multi-browser by default |
## Installation
### Backend (Go)
# Initialize module
# Dev tools
### Frontend (Vue 3)
### Infra (development)
# Single docker-compose for dev: ClickHouse + Postgres + FastNetMon
### Infra (production)
# ISP-style install on a Debian/Ubuntu coletor box:
# then drop mitigador .deb (built by goreleaser)
## Alternatives Considered
### Flow Collectors
| Recommended | Alternative | When to Use Alternative |
|---|---|---|
| **FastNetMon Community** (engine + collector) | **pmacct** (`nfacctd`, `sfacctd`) | When you need BMP/BGP route enrichment beyond what FastNetMon does, or super-flexible aggregation/export. pmacct is C, very fast, but no built-in DDoS detection — you'd reimplement it |
| **FastNetMon Community** | **GoFlow2** | When detection logic must be 100% custom in Go and you don't want a C++ dependency. Trade-off: you reimplement per-host counters, threshold engine, and attack classification |
| **FastNetMon Community** | **nfdump / nfcapd** | Only for archival/forensic use. nfdump is excellent at storing/querying NetFlow files, but it's not a real-time detection tool |
| **FastNetMon Community** | **Akvorado** | When the goal is visibility/analytics first, mitigation second. Akvorado is a polished collector+UI on ClickHouse but has no built-in DDoS response — would still need GoBGP layer on top |
### BGP Speakers
| Recommended | Alternative | When to Use Alternative |
|---|---|---|
| **GoBGP** (gRPC + Go library) | **ExaBGP 4.x** | When the team is Python-native or wants the simplest possible "shell out to a script" model. ExaBGP is the historical RTBH/Flowspec workhorse and is widely deployed. Trade-off: requires Python runtime, less performant for high-route-volume |
| **GoBGP** | **BIRD 2.x** | When you also need full-router functionality (route reflector, IXP, complex policy). For pure mitigation announce, BIRD is overkill; its config language is its own DSL |
| **GoBGP** | **FRR** | Only if Mitigador is going to act as a real router — not the use case. FRR is enormous, vtysh-based, designed for full router replacement |
### Backend Language
| Recommended | Alternative | When to Use Alternative |
|---|---|---|
| **Go** | **Python 3.12** | If the team has zero Go experience. Pairs naturally with ExaBGP. Trade-off: slower flow processing if doing it in-process, harder ops (interpreter + venv vs single binary) |
| **Go** | **Rust** | If you want zero-cost abstractions and the team has Rust expertise. Trade-off: BGP ecosystem in Rust is immature (no GoBGP equivalent); build time and cognitive cost are higher; MVP velocity drops |
### Storage
| Recommended | Alternative | When to Use Alternative |
|---|---|---|
| **ClickHouse + Postgres** (split) | **TimescaleDB only** | When ops team strongly prefers a Postgres-only world. TimescaleDB handles both relational and time-series acceptably. Trade-off: 20x more disk than ClickHouse for the same flow volume, slower analytical queries |
| **ClickHouse + Postgres** | **InfluxDB 3** | When you want push-based ingestion with built-in alerting (Flux). Trade-off: InfluxDB 3 is a rewrite, less battle-tested for flow scale; weaker JOIN story |
| **ClickHouse + Postgres** | **Postgres only** (no TSDB) | For very small ISPs (< few k flows/sec). Acceptable if you aggregate aggressively before insert and don't keep raw flows. Trade-off: scaling ceiling hits fast |
### Frontend
| Recommended | Alternative | When to Use Alternative |
|---|---|---|
| **Vue 3** | **React 19 + Vite** | When the team already knows React or you want the deepest component ecosystem (shadcn/ui, MUI, Ant). Equally defensible for this project |
| **Vue 3** | **SvelteKit** | If you want the smallest bundle + fastest runtime. Trade-off: smaller component ecosystem; for an ops dashboard with charts + tables this matters less, but hiring/onboarding is harder |
| **SSE for real-time** | **WebSockets** | Only if dashboard needs to send commands back (e.g., manual approve mitigation from UI). Even then, do commands via REST and keep updates on SSE |
### Notifications
| Recommended | Alternative | When to Use Alternative |
|---|---|---|
| **go-telegram/bot** | **tucnak/telebot v4** | If you want a more "framework-y" routing/keyboard DSL. Both are well-maintained |
| **wneessen/go-mail** | **Resend / Postmark API** | If you have an internet-facing SMTP relay problem (deliverability). For ISP internal alerts, plain SMTP to ops mailbox is fine |
### Deployment
| Recommended | Alternative | When to Use Alternative |
|---|---|---|
| **Single binary + systemd** | **Docker Compose** | For lab/eval/dev only. Production ISP coletor box should be bare-metal Linux with systemd. ISPs already know systemd; Docker on the data-plane box adds an attack surface |
| **systemd** | **Kubernetes** | Never, for this use case. The coletor must sit close to the routers and the BGP session is anchored to a host; K8s adds complexity with zero payoff |
## What NOT to Use
| Avoid | Why | Use Instead |
|---|---|---|
| **`go-telegram-bot-api/telegram-bot-api`** | Unmaintained since 2021, missing recent Bot API features | `go-telegram/bot` |
| **`go-gomail/gomail`** | Maintenance mode, last meaningful release 2021 | `wneessen/go-mail` |
| **Fiber (fasthttp)** for the API | `fasthttp` is incompatible with `net/http` middleware, no HTTP/2, custom Request/Response types. Pain when integrating with stdlib-based libs (SSE, OpenTelemetry, prometheus exporter) | `chi` on stdlib `net/http` |
| **InfluxDB 1.x** | EOL, OSS path is now InfluxDB 3 which is a rewrite. Old training data may steer you here | ClickHouse |
| **Quagga** as BGP speaker | Effectively dead; FRR is the fork that's maintained | GoBGP for our use, FRR if you really need a full router |
| **MongoDB** for incidents | Wrong tool — incidents are relational (incident has mitigations, mitigations target hosts, hosts belong to tenants) | PostgreSQL |
| **`net/smtp` (stdlib only)** for alerts | Missing modern auth methods, not concurrency-safe, no helpers for HTML/attachments | `wneessen/go-mail` (which extends stdlib correctly) |
| **`logrus`, `zap`** for new code | Stdlib `log/slog` is stable since Go 1.21 and idiomatic | `log/slog` |
| **GORM** | Slow, complicated, hides SQL — bad fit for an ops tool where you want exact query control | `sqlc` + `pgx` directly |
| **Forking `lupael/ddos-protection`** | **The repository does not exist** (verified 404). Premise of `PROJECT.md` is wrong | Build greenfield on FastNetMon Community + GoBGP (recommended) |
| **Custom sFlow parsing on Mikrotik** | **Mikrotik RouterOS has no sFlow support** as of 2026 — only NetFlow v1/v5/v9 and IPFIX (`PROJECT.md` assumption is wrong) | Use NetFlow v9 / IPFIX from Mikrotik; sFlow only for Juniper/Cisco that support it |
| **Relying on BGP Flowspec on Mikrotik** | Mikrotik RouterOS v7 (as of 2026) does **not** support BGP Flowspec — confirmed open feature request. RTBH works, Flowspec does not | Use RTBH on Mikrotik; reserve Flowspec for Juniper/Cisco peers |
| **Acting as inter-AS Flowspec source** to upstreams | Few transit providers accept Flowspec from customers. Validate with each upstream before designing on it | Flowspec is for intra-AS to your own edge routers; RTBH (with community signaling) for upstreams |
## Stack Patterns by Variant
- FastNetMon Community runs as a separate process (its own systemd unit), configured via `fastnetmon.conf`
- Mitigador's Go service watches FastNetMon's notify hook (script-based: FastNetMon calls `/usr/local/bin/mitigador-notify` on attack start/end with attack metadata as args/stdin)
- Mitigador handles: BGP announce via embedded GoBGP, Telegram/email fan-out, dashboard SSE push, ClickHouse archival
- Pro: 80% less code, proven detection
- Con: Detection logic is FastNetMon's threshold model (not pluggable)
- Embed GoFlow2 as a library; deserialize flows to a per-host counter map (sharded by `dst_ip`)
- Implement EWMA + threshold + N-of-M trigger in Go
- Pro: Full control, can add anomaly detection / ML later
- Con: 2-3x more dev time for MVP, you re-derive what FastNetMon already does
- Don't try multi-org-in-one-binary
- Run separate systemd instances with `mitigador@isp.service`, `mitigador@cliente.service` (systemd instantiated units)
- Each instance has its own `/etc/mitigador/instances/<name>/config.yaml`, own DB schema, own BGP session
- ClickHouse can be shared (separate databases per tenant)
- Telegram alerts won't work directly — provide a relay/HTTP webhook to a jump host
- Email goes to internal SMTP relay
- Bundle all binaries + ClickHouse + Postgres in an offline installer (goreleaser supports this)
## Version Compatibility
| Package A | Compatible With | Notes |
|---|---|---|
| Go 1.22+ | GoBGP v4.x | GoBGP v4 requires Go 1.21+; v3 supports older Go |
| FastNetMon Community 1.2.8 | ExaBGP 4.x **and** GoBGP v3+ | FastNetMon shells out to either — you choose. Recommend GoBGP since you're already Go |
| ClickHouse 24.x | clickhouse-go v2.27+ | v1 driver is EOL; do not use |
| Postgres 16 | pgx v5.5+ | Earlier pgx works but missing some pool tunables |
| Vue 3.4 | Naive UI 2.39+ | Naive UI 2.40+ requires Vue 3.4+ |
| Vite 5 | Vue plugin `@vitejs/plugin-vue` 5.x | Don't mix Vite 5 with the v4 plugin |
| go-telegram/bot (Mar 2026) | Bot API 9.5 | If you need Bot API 9.6+ features, check the lib changelog before use |
## Sources
### High Confidence (Official docs, recent releases, repo state verified)
- [FastNetMon GitHub repo (pavel-odintsov/fastnetmon)](https://github.com/pavel-odintsov/fastnetmon) — verified version 1.2.8, 3.7k★, C++ codebase
- [FastNetMon Community vs Advanced comparison](https://fastnetmon.com/compare-community-and-advanced/) — Community lacks Flowspec, web UI, email alerts natively
- [FastNetMon Pentanet ISP case study (April 2026)](https://fastnetmon.com/2026/04/06/case-pentanet-real-time-ddos-detection-at-the-edge/) — production ISP deployment evidence
- [GoBGP releases](https://github.com/osrg/gobgp/releases) — verified v4.5.0 (Apr 2024) as latest tagged release; pkg.go.dev confirms ongoing development
- [GoBGP Flowspec documentation](https://github.com/osrg/gobgp/blob/master/docs/sources/flowspec.md) — RFC 5575 + v6 support verified
- [GoFlow2 GitHub (netsampler/goflow2)](https://github.com/netsampler/goflow2) — verified v2.2.6 (Dec 2025), active maintenance
- [MikroTik Traffic Flow docs](https://help.mikrotik.com/docs/spaces/ROS/pages/21102653/Traffic+Flow) — confirms NetFlow v1/v5/v9 + IPFIX, **no sFlow**
- [MikroTik sFlow feature request (still open)](https://forum.mikrotik.com/t/add-sflow/142506) — confirms sFlow unsupported in 2026
- [MikroTik BGP Flowspec feature request](https://forum.mikrotik.com/t/request-feature-bgp-dynamic-neighbors-bgp-flowspec/149201) — confirms Flowspec missing in RouterOS v7
- [Cisco IOS XR BGP Flowspec docs](https://www.cisco.com/c/en/us/td/docs/iosxr/cisco8000/bgp/b-bgp-config-cisco8000/m-bgp-flowspec.html) — confirms full Flowspec support
- [Juniper "Day One: Deploying BGP Flowspec"](https://www.juniper.net/documentation/en_US/day-one-books/DO_BGP_FLowspec.pdf) — confirms full Flowspec support
- [Akvorado GitHub](https://github.com/akvorado/akvorado) — ClickHouse-based, AGPLv3, alternative architecture for comparison
- [wneessen/go-mail](https://github.com/wneessen/go-mail) — modern Go SMTP lib, low deps
- [go-telegram/bot](https://github.com/go-telegram/bot) — supports Bot API 9.5, March 2026 release
### Verified Repo Non-Existence
- `https://github.com/lupael/ddos-protection` — **HTTP 404**, verified via WebFetch on 2026-05-17
- Web search across multiple engines returns no matches for "lupael/ddos-protection"
- User `lupael`'s public repos: `IPTV`, `mikhmon`, `mikrotik`, `freeradius-advanced`, `librenms`, `cacti`, `Easypayway` — none contain DDoS protection logic
### Medium Confidence (Multi-source synthesis, no single canonical doc)
- [ClickHouse vs TimescaleDB vs InfluxDB 2026 benchmarks (sanj.dev)](https://sanj.dev/post/clickhouse-timescaledb-influxdb-time-series-comparison) — compression and write throughput numbers
- [SSE vs WebSockets guide (Railway docs)](https://docs.railway.com/guides/sse-vs-websockets) — SSE recommended for dashboards
- [Open-source BGP stacks shootout (Elegant Network)](https://elegantnetwork.github.io/posts/comparing-open-source-bgp-stacks/) — performance comparison (note: GoBGP slower at bulk route ingest, irrelevant for our small route-set use case)
- [Go web frameworks comparison 2026 (Encore)](https://encore.dev/articles/gin-vs-echo-vs-fiber) — Gin/Echo most popular; chi recommended for stdlib alignment
- [Vue 3 vs React 19 admin dashboard analysis (multiple 2026 sources)](https://www.thefrontendcompany.com/posts/vue-vs-react)
- [Single-binary Go + systemd deployment (amazingcto.com)](https://www.amazingcto.com/simplicity-of-golang-systemd-deployments/) — argues for systemd over Docker for ops tooling
### Lower Confidence (Single source, blog/community)
- [Flowtriq DDoS detection comparison 2026](https://flowtriq.com/blog/fastnetmon-vs-wanguard-vs-flowtriq) — competitor blog, useful for landscape but biased
- [bizety 2025 routing daemons guide](https://bizety.com/2025/10/09/2025-guide-to-open-source-routing-daemons-frr-bird-and-exabgp/) — single-source overview
- "Akvorado ISP deployment review" — single hostkey.com source for production sizing recommendations
## Action Items for Roadmap
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->
## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, or `.github/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->



<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
