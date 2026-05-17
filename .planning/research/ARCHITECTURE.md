# Architecture Research

**Domain:** DDoS detection and mitigation platform for small/medium ISPs (volumetric, BGP-based)
**Researched:** 2026-05-17
**Confidence:** HIGH (component patterns, data flow, BGP); MEDIUM (specific throughput numbers); LOW (lupael/ddos-protection reference repo — could not verify it exists publicly, GitHub returns 404)

## Standard Architecture

### System Overview

```
┌────────────────────────────────────────────────────────────────────────┐
│                       NETWORK EDGE (out of scope)                       │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐                │
│  │ Mikrotik     │   │ Juniper MX   │   │ Cisco ASR    │   ...           │
│  │ (sFlow v5)   │   │ (sFlow/IPFIX)│   │ (NetFlow v9) │                │
│  └──────┬───────┘   └──────┬───────┘   └──────┬───────┘                │
│         │ UDP/6343         │ UDP/4739         │ UDP/2055                │
└─────────┼──────────────────┼──────────────────┼────────────────────────┘
          │                  │                  │
          ▼                  ▼                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│                       INGESTION LAYER                                   │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │  Flow Collector(s) — UDP listeners + template cache              │  │
│  │  - sFlow v5 decoder      (port 6343)                             │  │
│  │  - NetFlow v5/v9 decoder (port 2055)                             │  │
│  │  - IPFIX decoder         (port 4739)                             │  │
│  │  → normalize to common flow record schema                        │  │
│  └────────────────────────┬─────────────────────────────────────────┘  │
└───────────────────────────┼──────────────────────────────────────────────┘
                            │ in-process channel / pub-sub
                            ▼
┌────────────────────────────────────────────────────────────────────────┐
│                       HOT PATH (detection)                              │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │  Aggregator: per-host / per-prefix sliding window counters       │  │
│  │  Counters: pps, bps, fps, per-protocol (UDP/ICMP/TCP/SYN)        │  │
│  │  Window: 1-5s buckets, last N seconds in memory                  │  │
│  └────────────────────────┬─────────────────────────────────────────┘  │
│                           │                                              │
│  ┌────────────────────────▼─────────────────────────────────────────┐  │
│  │  Detection Engine: threshold evaluator                           │  │
│  │  - per-prefix / per-tenant thresholds (not global)               │  │
│  │  - attack classification (UDP flood / ICMP flood / amplification)│  │
│  │  - emit Attack event with confidence + vector + target IP        │  │
│  └────────────────────────┬─────────────────────────────────────────┘  │
└───────────────────────────┼──────────────────────────────────────────────┘
                            │ Attack event (in-process or message bus)
              ┌─────────────┼─────────────┐
              ▼             ▼             ▼
┌─────────────────────┐ ┌──────────────┐ ┌────────────────────────────────┐
│  MITIGATION ENGINE  │ │  ALERTING    │ │  PERSISTENCE                    │
│  - decide RTBH vs   │ │  - Telegram  │ │  - Incident record (cold path)  │
│    Flowspec         │ │  - Email     │ │  - Time-series counters (warm)  │
│  - build NLRI       │ │  - Dashboard │ │                                 │
│  - call BGP speaker │ │    websocket │ │                                 │
└──────────┬──────────┘ └──────────────┘ └────────────────────────────────┘
           │ gRPC / config-file / API
           ▼
┌────────────────────────────────────────────────────────────────────────┐
│                       BGP SPEAKER                                       │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │  GoBGP / ExaBGP / BIRD2 daemon                                   │  │
│  │  - eBGP session to ISP edge routers (own ASN or 64512+)          │  │
│  │  - announces:                                                    │  │
│  │      * RTBH: /32 to-blackhole, community 65535:666 (RFC 7999)    │  │
│  │      * Flowspec: src=any, dst=victim/32, proto=UDP, port=53 → drop│ │
│  │  - withdraws when attack ends                                    │  │
│  └────────────────────────┬─────────────────────────────────────────┘  │
└───────────────────────────┼──────────────────────────────────────────────┘
                            │ BGP UPDATE over TCP/179
                            ▼
                  [ Back to edge routers → drop at line rate ]

┌────────────────────────────────────────────────────────────────────────┐
│                       CONTROL PLANE                                     │
│  ┌─────────────────┐  ┌─────────────────┐  ┌────────────────────────┐  │
│  │  REST/GraphQL   │  │  Web Dashboard  │  │  Storage               │  │
│  │  API            │  │  (SPA + WS)     │  │  - SQL: incidents,     │  │
│  │  - tenant CRUD  │  │  - live attacks │  │    config, users       │  │
│  │  - threshold    │  │  - history view │  │  - TSDB: counters,     │  │
│  │    config       │  │  - manual       │  │    flow metrics        │  │
│  │  - approve/deny │  │    mitigation   │  │                        │  │
│  └─────────────────┘  └─────────────────┘  └────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|----------------|------------------------|
| Flow Collector | Listen on UDP/6343/2055/4739, decode datagrams, manage NetFlow/IPFIX template cache, normalize to common record | Single binary with goroutines/async tasks per port; or use goflow2 as library |
| Aggregator | Maintain per-host/per-prefix sliding window counters (pps, bps, fps, per-proto) | In-memory map, ring buffer per host, last 30-60s of buckets |
| Detection Engine | Compare aggregated counters vs configured thresholds; classify attack vector; emit Attack event | Pure-function evaluator iterating hosts each tick (1s); per-tenant threshold lookup |
| Mitigation Engine | Decide RTBH vs Flowspec, build NLRI/community, call BGP speaker; withdraw on attack-end; optional manual-approval gate | Decision tree on attack volume + tenant policy; dry-run mode |
| BGP Speaker | Maintain eBGP session(s) to edge routers; announce/withdraw routes; reflect peer state | GoBGP (gRPC API) or ExaBGP (stdin/stdout); embedded or sidecar |
| Alert Dispatcher | Fan-out Attack event to Telegram bot, SMTP, dashboard websocket | Worker pool; retry queue; per-tenant channel config |
| Storage | Persist incident history (cold), recent flow counters (warm), config (relational) | PostgreSQL for relational+cold; Redis or local TSDB (e.g. VictoriaMetrics, embedded SQLite) for warm |
| Dashboard/API | Operator UI; CRUD config; live view; manual mitigation; multi-tenant isolation | REST/GraphQL backend + SPA frontend; WebSocket for live attacks |

## Recommended Project Structure

This structure assumes Go as core language (a common choice for this domain — GoBGP, goflow2, vflow, FastNetMon-adjacent tooling). Adapt to chosen stack.

```
mitigador/
├── cmd/
│   ├── mitigador/           # main daemon entrypoint (single binary MVP)
│   ├── mitigador-cli/       # ops CLI: show attacks, manual block/unblock
│   └── mitigador-api/       # optional separate API server (if split later)
├── internal/
│   ├── ingest/              # flow collectors
│   │   ├── sflow/           # sFlow v5 decoder + UDP listener
│   │   ├── netflow/         # NetFlow v5/v9 decoder + template cache
│   │   ├── ipfix/           # IPFIX decoder
│   │   └── normalize/       # common flow record schema, conversion
│   ├── aggregate/           # per-host/prefix sliding-window counters
│   │   ├── buckets.go       # ring buffer of 1s buckets
│   │   └── hostgroup.go     # tenant/prefix-aware grouping
│   ├── detect/              # detection engine
│   │   ├── thresholds.go    # per-tenant threshold config eval
│   │   ├── classify.go      # UDP-flood / ICMP-flood / amplification heuristics
│   │   └── event.go         # AttackStarted/AttackEnded event types
│   ├── mitigate/            # mitigation decisions
│   │   ├── policy.go        # RTBH vs Flowspec choice, manual-approval gate
│   │   └── controller.go    # lifecycle: start, refresh, withdraw
│   ├── bgp/                 # BGP speaker integration
│   │   ├── gobgp.go         # gRPC client to embedded/sidecar GoBGP
│   │   └── nlri.go          # build RTBH route, build Flowspec NLRI
│   ├── alert/               # multi-channel dispatcher
│   │   ├── telegram/        # bot HTTP client
│   │   ├── email/           # SMTP sender
│   │   └── webhook/         # generic webhook (future)
│   ├── api/                 # HTTP API + websocket for dashboard
│   │   ├── handlers/        # REST/GraphQL handlers
│   │   ├── auth/            # tenant auth, RBAC
│   │   └── ws/              # live attack feed
│   ├── storage/             # persistence
│   │   ├── postgres/        # incidents, config, users
│   │   ├── timeseries/      # counters retention (warm path)
│   │   └── migrations/
│   ├── tenant/              # multi-tenant isolation primitives
│   └── config/              # YAML/TOML loader, hot reload
├── web/                     # SPA (React/Vue/Svelte) dashboard
├── deploy/
│   ├── systemd/             # service units
│   ├── docker/              # compose for dev + reference prod
│   └── ansible/             # ISP deployment playbook (optional)
└── docs/
    ├── router-config/       # Mikrotik/Juniper/Cisco BGP+flow snippets
    └── runbooks/            # operator runbooks
```

### Structure Rationale

- **cmd/ vs internal/:** Go convention; `internal/` prevents accidental import as library. Single `mitigador` binary keeps MVP deployment simple (one process, one config file, one systemd unit). API can split later if needed.
- **ingest/ split per protocol:** Each flow protocol has very different parsing (sFlow is fixed format, NetFlow v9/IPFIX are template-driven and stateful). Isolation lets you test/replace independently.
- **aggregate/ separate from detect/:** Counter aggregation is hot and CPU-bound (every flow record updates buckets); detection is periodic (every 1s tick). Separating allows different concurrency models.
- **bgp/ as own package:** BGP integration is the highest-risk dependency. Keeping it behind an interface lets you swap GoBGP ↔ ExaBGP ↔ direct BGP implementation, and lets you build a `dry-run` speaker for tests.
- **deploy/router-config/:** Router-side snippets are part of the product — operators need copy-paste configs to expose sFlow and accept the BGP session.

## Architectural Patterns

### Pattern 1: Pipeline with In-Process Channels (MVP)

**What:** Single binary, components communicate via buffered channels (Go) or async queues. Collector → Aggregator → Detector → Mitigator runs as one process.

**When to use:** Small/medium ISP (1-10 Gbps borders). One server is sufficient. Avoids ops burden of Kafka/Redis for MVP.

**Trade-offs:**
- Pros: Simple deployment (single systemd unit), low latency (in-memory), easy to debug, no inter-service auth needed.
- Cons: Vertical scaling only; crash loses in-flight state; harder to scale collector independently.

**Example (Go, conceptual):**
```go
// main.go
flowCh := make(chan FlowRecord, 100_000)   // buffered, drops on overflow
attackCh := make(chan AttackEvent, 1000)

go sflow.Listen(":6343", flowCh)
go netflow.Listen(":2055", flowCh)
go ipfix.Listen(":4739", flowCh)

agg := aggregate.New()
go agg.Consume(flowCh)                      // updates per-host buckets

det := detect.New(thresholds, attackCh)
go det.Tick(agg, 1*time.Second)             // evaluates every 1s

mit := mitigate.New(bgpClient)
alert := alert.New(telegramBot, smtp)
for ev := range attackCh {
    go mit.Handle(ev)
    go alert.Dispatch(ev)
    go storage.Record(ev)
}
```

### Pattern 2: BGP Speaker as Embedded Library (vs Sidecar)

**What:** Use GoBGP (or BIRD2) embedded in-process via library/gRPC, instead of running a separate BGP daemon and shelling out.

**When to use:** Always for greenfield Go projects — GoBGP is designed for this. ExaBGP-as-sidecar is the pattern for non-Go projects.

**Trade-offs:**
- Embedded (GoBGP gRPC): tight integration, atomic announce/withdraw, single process to monitor. Heavier binary.
- Sidecar (ExaBGP): language-agnostic (Python/Perl/anything that writes stdout), separates BGP-state crash from app. Requires two processes, IPC fragility.

**Reference:** AMS-IX deployment combines FastNetMon (C++) + Python orchestrator + BIRD2 sidecar, achieving ~45s end-to-end mitigation. Embedded approach can do faster (sub-second) because no shell-out overhead.

### Pattern 3: Per-Tenant Threshold Isolation (Multi-Tenant Pattern)

**What:** Configuration model treats each tenant (ISP-as-self, corporate-client-A, corporate-client-B) as an isolated `hostgroup` with its own prefixes, thresholds, BGP peer, and alert channels. Single binary, multiple tenant scopes.

**When to use:** When the same binary serves both ISP and corporate clients (project goal), but each deployment is a separate install (per `PROJECT.md`). Even single-deployment installs benefit from hostgroup isolation per /24 prefix.

**Trade-offs:**
- Pros: No threshold pollution across customers; per-tenant alert routing; per-tenant BGP session if needed.
- Cons: Config complexity; threshold tuning becomes per-tenant operational task.

**Note:** FastNetMon calls this "hostgroup" — direct analogue.

### Pattern 4: Hot Path / Cold Path Split (Lambda-lite)

**What:** Hot path keeps last 60s of per-host counters in memory (RAM) for detection. Cold path writes attack events (start, peak, end, sample flows) to durable storage. No need to persist every flow record.

**When to use:** Always. Persisting every flow at 100k+ flows/sec would crush any database; persisting only attack incidents (10s-100s/day) is trivial.

**Trade-offs:**
- Pros: Detection latency unaffected by storage; storage cost bounded; recent counters survive only by design.
- Cons: Cannot do retrospective "show me flows from 2 hours ago" — only attack-window samples are kept. Acceptable for MVP.

## Data Flow

### Detection Flow (Hot Path)

```
[Edge Router] --UDP sFlow datagram--> [Collector UDP listener]
       ↓
   decode (with NetFlow/IPFIX template lookup)
       ↓
   normalize → FlowRecord{src, dst, proto, pkts, bytes, sample_rate}
       ↓
   in-process channel
       ↓
[Aggregator] — multiply by sample_rate, update bucket[host][now_second]
       ↓
   every 1s tick
       ↓
[Detector] — read last N buckets, compute pps/bps/per-proto rate
       ↓
   compare vs tenant thresholds for that prefix
       ↓
   if exceeded → emit AttackEvent
       ↓
   ┌────────────┬────────────────┬───────────────────┐
   ▼            ▼                ▼                   ▼
[Mitigator]  [Alerter]      [Storage]         [Dashboard WS]
   ↓
build BGP NLRI (RTBH /32 or Flowspec match+action)
   ↓
gRPC → [GoBGP speaker] → BGP UPDATE → [Edge Router FIB] → traffic dropped
```

**Latency budget (target, MVP):**
- Edge router sample → collector socket: 50-500ms (network + sFlow batching)
- Collector decode → bucket update: <10ms
- Detector tick interval: 1s (configurable)
- AttackEvent → mitigator → BGP UPDATE sent: <500ms
- BGP UPDATE → edge router FIB programmed: 1-5s (router-dependent)
- **Total: 3-7 seconds from attack start to mitigation active** (achievable; FastNetMon reports under 2s in some deployments; AMS-IX hits ~45s end-to-end including external scrubber handoff)

### Withdrawal Flow

```
[Detector] — no longer exceeding threshold for grace period (e.g. 60s)
       ↓
emit AttackEnded event
       ↓
[Mitigator] — BGP WITHDRAW for the NLRI
       ↓
[Edge Router] — removes blackhole / Flowspec rule
       ↓
[Storage] — finalize incident record with end timestamp + peak stats
       ↓
[Alerter] — send "attack ended" notification
```

### State Management

```
Persistent (PostgreSQL):
- tenants, users, BGP peer configs, threshold profiles, channel configs
- incidents (one row per attack, with peak pps/bps, vector, duration, action taken)
- audit log (who approved manual mitigation, etc.)

Warm (in-memory or Redis):
- last 60s of per-host counter buckets
- active attack state (which prefixes are currently mitigated)
- BGP speaker peer state

Ephemeral (never persisted):
- raw flow records (consumed by aggregator and dropped)
- NetFlow/IPFIX template cache (rebuilt from router re-sends on restart)
```

### Key Data Flows

1. **Flow ingestion:** Router → UDP datagram → collector → decoder → normalized FlowRecord → aggregator buckets. Volume: thousands to tens of thousands of flow records/sec at small ISP.
2. **Detection tick:** Every 1s, detector iterates active hosts, evaluates thresholds, emits 0-N AttackEvents.
3. **Mitigation announcement:** AttackEvent → mitigator → GoBGP gRPC → BGP UPDATE to edge router. Single message; idempotent.
4. **Alert fan-out:** AttackEvent → N alert channels in parallel; each retries independently on failure.
5. **Dashboard updates:** Active attacks pushed via WebSocket; historical queries via REST against PostgreSQL.

## Scaling Considerations

| Scale | Architecture Adjustments |
|-------|--------------------------|
| 1 ISP, 1-10 Gbps border | Single binary, single server. PostgreSQL + Redis (or just SQLite) on same host. In-process channels. This is the MVP target. |
| 1 ISP, 10-100 Gbps border | Still single binary; ensure NIC buffers tuned (`net.core.rmem_max`), enable FastNetMon-style parallel collectors per port. Move PostgreSQL to separate host. |
| Multi-ISP / 100+ Gbps borders | Split collector from detector; introduce Kafka/Redpanda between them. Run multiple collector pods. Detector still single-instance per tenant (state is per-host counters). BGP speaker remains one per edge-router-group. |
| Geographic distribution | One full stack per POP; central dashboard federates via API. |

### Throughput Reference Points (sources)

- sFlow at 1:1000 sampling on 10 Gbps with ~150B avg packets: ~8000 samples/sec/interface (derived from sFlow.org math). Most networks use 1:512 to 1:4096 sampling [sFlow / Wikipedia].
- goflow2 handles "decent" throughput per-port; horizontal scaling via multiple sockets [goflow2 README].
- FastNetMon reference: 45 Tbps / 1.5 Gpps in production, 3M flows/sec internal benchmark [FastNetMon scalability docs] — far exceeds small-ISP needs; useful as ceiling reference.
- AMS-IX production pipeline: ~45s end-to-end IPv4 mitigation (FastNetMon + Python + BIRD2 + external scrubber) [FastNetMon AMS-IX deep-dive, Jan 2026].

### Scaling Priorities

1. **First bottleneck (most likely):** UDP socket receive buffer overflow on collector when sFlow burst arrives. Fix: increase `net.core.rmem_max` to 16MB+, use SO_REUSEPORT for parallel sockets per port, drop oldest on overflow.
2. **Second bottleneck:** Single-threaded aggregation when host count goes into millions (unlikely for small ISP). Fix: shard aggregation by hash of host IP across goroutines/cores.
3. **Third bottleneck:** Database write rate if persisting too much. Fix: batch incident writes; never persist raw flow records.

## Anti-Patterns

### Anti-Pattern 1: Persisting Raw Flow Records

**What people do:** Write every parsed flow to PostgreSQL or even Elasticsearch for "historical analysis."
**Why it's wrong:** A small ISP with 1:1000 sampling on a 10G link emits ~8k flows/sec per interface. Multiply across 4 interfaces and 86400 seconds = ~2.7B rows/day. Database fills disk in days; queries become unusable.
**Do this instead:** Aggregate in memory, persist only (a) per-minute summary counters to a TSDB and (b) full detail only for the attack window (last N minutes around an incident, captured as a pcap-style sample).

### Anti-Pattern 2: Global Static Thresholds

**What people do:** One `pps_threshold: 100000` config value applies to every host.
**Why it's wrong:** A web hosting customer normally pushes 50kpps; a residential CPE normally pushes 100pps. Global threshold either misses real attacks on small customers or wakes the operator at 3am for normal traffic on the hosting customer.
**Do this instead:** Per-prefix / per-hostgroup thresholds. Provide reasonable defaults but require operator to tune. Optional: baseline learning over 7-14 days to suggest values.

### Anti-Pattern 3: Auto-Mitigating Without an Approval Mode

**What people do:** Ship MVP with "auto-blackhole every detected attack" enabled by default.
**Why it's wrong:** First false positive blackholes a legit customer; trust in the tool dies. `PROJECT.md` explicitly flags this risk.
**Do this instead:** Three modes per tenant: `dry-run` (alert only), `manual-approve` (alert + 1-click approval), `auto` (full automation). Default new installs to `manual-approve`. Move to `auto` after threshold confidence.

### Anti-Pattern 4: Coupling Detection to BGP Speaker

**What people do:** Detection logic directly calls "send BGP UPDATE" with hardcoded route building.
**Why it's wrong:** Cannot swap BGP backend (GoBGP → ExaBGP → direct), cannot dry-run, cannot test detection without spinning up a BGP peer.
**Do this instead:** Detection emits `AttackEvent`; mitigation engine consumes events and talks to a `BGPSpeaker` interface. Provide a `NoopSpeaker` for tests and a `LoggingSpeaker` for dry-run.

### Anti-Pattern 5: Ignoring NetFlow/IPFIX Template State

**What people do:** Treat NetFlow v9 like sFlow — decode each datagram independently.
**Why it's wrong:** NetFlow v9 / IPFIX use templates sent periodically by the router. Data records arriving before a template (e.g., after collector restart) cannot be decoded. Without a template cache, you silently lose flows.
**Do this instead:** Maintain in-memory template cache keyed by (router IP, observation domain ID, template ID). Buffer data records briefly if template not yet seen. Log when templates expire / are re-sent.

### Anti-Pattern 6: One Big Tenant

**What people do:** Run one shared instance for "ISP + 5 corporate clients" with no isolation.
**Why it's wrong:** Conflicting threshold needs, alert noise, BGP-session security blur, hard to give a customer their own dashboard. `PROJECT.md` decided against shared multi-org for this reason.
**Do this instead:** One install per deployment (ISP install separate from corporate-client install). Within an install, use tenant scoping for prefixes/thresholds/channels but don't pretend it's a SaaS.

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| Edge router (Mikrotik / Juniper / Cisco) — flow export | UDP listener (we are passive receiver) on 6343/2055/4739 | Router config is operator's responsibility; ship copy-paste snippets in `deploy/router-config/`. Verify each vendor's sampling-rate field semantics differ (sFlow `sampling_rate` vs Cisco NetFlow `sampler-id`). |
| Edge router — BGP session | eBGP, TCP/179, MD5 auth recommended | We initiate or accept based on router policy. Use private ASN (64512-65534) for the mitigator; advertise only mitigation routes; never accept routes. |
| Telegram | HTTPS to Bot API (api.telegram.org); long-poll or webhook | Bot token per install; one channel/chat per tenant. |
| SMTP | TLS to relay (operator-provided or local Postfix) | Per-tenant from-address + recipient list. |
| Web browser (dashboard) | HTTPS + WebSocket for live updates | Auth via session cookie or JWT; ensure WS reconnect on drop. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| Collector ↔ Aggregator | In-process buffered channel (Go) / async queue | Drop oldest on overflow; expose drop counter as metric. |
| Aggregator ↔ Detector | Shared in-memory state, read by detector tick goroutine | Detector reads counter snapshots; aggregator writes. Use atomic counters or sync.Map. |
| Detector ↔ Mitigator/Alerter/Storage | Event channel (AttackEvent) | Pub-sub style; multiple consumers; never block detector. |
| Mitigator ↔ BGP Speaker | gRPC (GoBGP) or stdin (ExaBGP) | Behind interface; swappable. Idempotent operations. |
| API ↔ Storage | Direct DB connection (single binary) or HTTP (if split) | Tenant-scope every query. |
| Dashboard ↔ Backend | REST for CRUD, WebSocket for live attack feed | Authenticate WS on connect. |

## Suggested Build Order (MVP → Beyond)

Critical: each component blocks something downstream. Build the spine before the limbs.

1. **Flow collector (sFlow first)** — Without flows, nothing else works. Ship a working sFlow collector that prints normalized records to stdout. Validate against a real Mikrotik or `sflowtool` generator. **Blocks everything.**
2. **NetFlow v9 + IPFIX collectors** — Same shape, but template-stateful. Can be parallel with #3 if separate dev. **Blocks Cisco and modern-router support.**
3. **Aggregator (per-host buckets)** — Convert flow stream to per-host pps/bps over time. Verify counters reasonable vs known traffic. **Blocks detection.**
4. **Detection engine (single threshold, single tenant)** — Hardcoded threshold, single host. Emit AttackEvent to a log. Validate by running a test UDP flood. **Blocks mitigation and alerting.**
5. **Telegram alerter** — Cheapest end-to-end signal. Operator gets a notification when detection fires. This makes the tool useful even with zero automated mitigation. **First user-visible value.**
6. **GoBGP speaker + RTBH** — Embed GoBGP, peer with a test router (or BIRD container), announce a /32 blackhole on detection, withdraw on attack end. Run in dry-run mode first. **Blocks real mitigation.**
7. **Storage (incidents table)** — PostgreSQL or SQLite; write one row per attack. **Blocks dashboard history.**
8. **Dashboard (read-only, live attacks)** — Minimal SPA showing current attacks + history table. WebSocket for live. **Blocks operator confidence in automated actions.**
9. **Per-tenant / per-prefix thresholds** — Replace hardcoded threshold with config. Multi-tenant model arrives here. **Blocks production use.**
10. **Email alerter** — Lower priority than Telegram for BR-ISP audience but needed for incident reports.
11. **Flowspec mitigation** — More complex NLRI; needed for granular blocks (block only UDP/53 to victim, leaving rest). RTBH covers MVP.
12. **Manual approval mode** — Pause auto-mitigation; require dashboard click. Reduces false-positive risk early in production.
13. **API for config CRUD** — Replace YAML editing with UI-driven config.
14. **NetFlow v5 (legacy Cisco)** — Lowest priority; covers only old gear.

**Critical path (smallest viable MVP):** 1 → 3 → 4 → 5 → 6 → 7 → 8. After step 8 you have: sFlow ingestion, host counters, detection, Telegram alert, BGP RTBH mitigation, persisted incidents, live dashboard. That is shippable to a friend's ISP for first real-world test.

## Sources

- [FastNetMon AMS-IX deep dive (Jan 2026)](https://fastnetmon.com/2026/01/27/engineering-deep-dive-how-ams-ix-uses-fastnetmon-for-automated-ddos-mitigation/) — real production pipeline, ~45s end-to-end IPv4 mitigation
- [FastNetMon scalability docs](https://fastnetmon.com/docs-fnm-advanced/fastnetmon-advanced-scalability/) — 45 Tbps / 1.5 Gpps / 3M flows/sec reference points
- [FastNetMon traffic buffer docs](https://fastnetmon.com/docs-fnm-advanced/traffic-buffer-capability-to-speed-up-attack-detection-speed/) — single-binary daemon architecture, single-thread default
- [FastNetMon BGP Flowspec config](https://fastnetmon.com/docs-fnm-advanced/fastnetmon-bgp-flow-spec-configuration/)
- [FastNetMon product overview](https://fastnetmon.com/product-overview/)
- [goflow2 README (netsampler)](https://github.com/netsampler/goflow2) — Go collector architecture (decoder → producer → format → transport), ports 2055/6343, Kafka output pattern
- [Edgio vflow NetFlow v9 decoder](https://github.com/Edgio/vflow/blob/master/netflow/v9/decoder.go) — reference Go NetFlow v9/IPFIX template handling
- [ExaBGP DDoS Mitigation wiki](https://github.com/Exa-Networks/exabgp/wiki/DDoS-Mitigation) — ExaBGP-as-sidecar pattern with FlowSpec NLRI
- [ExaBGP FastNetMon integration](https://github.com/Exa-Networks/exabgp/wiki/FastNetMon)
- [Wanguard BGP Connector docs](https://docs.andrisoft.com/wanguard/8.4/Configuration__Components__BGP_Connector.html) — commercial analogue showing GoBGP/ExaBGP/Quagga connector pattern
- [RFC 7999: BLACKHOLE Community](https://www.rfc-editor.org/rfc/rfc7999.html) — well-known BGP community 65535:666 for RTBH
- [RFC 5635: Remote Triggered Black Hole Filtering with uRPF](https://datatracker.ietf.org/doc/html/rfc5635) — RTBH foundational spec
- [Noction: BGP RTBH community](https://www.noction.com/blog/bgp-blackhole-community)
- [Cloudflare reference architecture: protecting SP networks from DDoS](https://developers.cloudflare.com/reference-architecture/diagrams/network/protecting-sp-networks-from-ddos/)
- [Kentik: detect DDoS attacks using flow analytics](https://www.kentik.com/kentipedia/detect-ddos-attacks-flow-analytics/)
- [ElastiFlow NetFlow/IPFIX/sFlow ports config](https://docs.elastiflow.com/6.4/docs/config_ref/flowcoll/input_udp/) — standard ports 2055/4739/6343
- [sFlow specification v5 (sflow.org)](https://sflow.org/sflow_version_5.txt) — sFlow datagram format, UDP/6343
- [sFlow overview (sflow.org)](https://sflow.org/sFlowOverview.pdf) — sampling theory, scalability to 10G+

**Note on `lupael/ddos-protection` reference:** The repository URL referenced in `PROJECT.md` returned HTTP 404 on both web fetch and GitHub API (`api.github.com/repos/lupael/ddos-protection`) on 2026-05-17. The user should verify the exact owner/repo name before basing the project on it. Recommendation: do not block research on this; proceed with the well-established architecture patterns above (FastNetMon-style + GoBGP), which the referenced repo would almost certainly resemble.

---
*Architecture research for: DDoS detection and mitigation platform for ISPs*
*Researched: 2026-05-17*
