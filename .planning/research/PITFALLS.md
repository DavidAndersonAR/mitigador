# Pitfalls Research

**Domain:** DDoS detection and mitigation platform for small/medium Brazilian ISPs (BGP RTBH + Flowspec, flow telemetry)
**Researched:** 2026-05-17
**Confidence:** HIGH for technical/operational pitfalls; MEDIUM for Brazilian legal pitfalls

## Critical Pitfalls

### Pitfall 1: Blackholing the wrong /32 — taking your own customer offline

**What goes wrong:**
A false positive (or a spoofed/attacker-induced trigger) causes the system to announce an RTBH /32 for a legitimate customer IP. Once the upstream accepts the community, all traffic to that customer is dropped at the transit edge. The customer experiences a total outage that looks identical to the attack they thought they were being protected from — except *the ISP itself* caused it. This is the reputation killer that makes operators distrust automatic mitigation forever after one incident.

**Why it happens:**
- Static thresholds that don't account for legitimate traffic spikes (Black Friday, live-stream events, software releases, large backups).
- Detection window too short: a 5-second burst of legitimate UDP (game traffic, voice, P2P) crosses threshold.
- No sanity check that the "victim" IP is actually a customer of the announcing ASN.
- Operator never tested mitigation in dry-run before enabling auto-action.
- No allow-list / VIP-list for revenue-critical customers requiring manual approval.

**How to avoid:**
- **Dry-run mode mandatory for first weeks**: log what *would* have been mitigated; compare to operator judgment before flipping the switch. FastNetMon implements this as `gobgp_flow_spec_announces_dry_run_mode`.
- **Per-prefix / per-customer thresholds** (already in PROJECT.md), not global. A 50 Mbps spike on a home customer is an attack; on a corporate customer it's noon traffic.
- **Multiple-criteria trigger**: require packets/sec AND bits/sec AND duration sustained (not just one metric crossing for one sample).
- **VIP/critical-customer allow-list** that requires manual approval (Telegram inline button "Approve / Reject") before announcing RTBH.
- **Confidence score** on every detection; only auto-act above HIGH confidence.
- **Time-of-day baselining**: weekday 9-18h baseline ≠ weekend 03h baseline.

**Warning signs:**
- Operator gets RTBH announcement and says "wait, that's our biggest client" → already bad.
- Mitigation triggered during a known event window (sports, game release) → threshold too low.
- Same /32 mitigated multiple times per day with no actual complaint → likely false positives.
- Mitigated traffic *was* dropping legitimate traffic visible in netflow → confirmed FP.

**Phase to address:**
Phase: **Mitigation engine** (must ship with dry-run as default + manual-approval mode before auto-mode). Phase: **Operations/UI** (approve/reject buttons, VIP list, baseline visualization).

---

### Pitfall 2: Sampling-rate blindness — missing attacks smaller than your sample resolution

**What goes wrong:**
With sFlow at 1:1000 sampling, a 1 Gbps attack of small (64-byte) packets at ~2 Mpps gives you only ~2000 sampled packets/sec — enough to detect. But a 100 Mbps attack against a 1 Gbps customer link gives only ~200 sampled packets/sec, which gets lost in noise. Worse: an attack of 1000 small bursts each lasting 200ms can completely evade sampling. ISP detects nothing; customer's link is degraded; trust in the platform collapses.

**Why it happens:**
- Operators copy the vendor's "recommended" sample rate (often 1:2000 for 10G ports) without thinking about what attack sizes that hides.
- "Recommended sample rate" is sized for *traffic accounting*, not *attack detection*.
- Mikrotik RouterOS v6.49.6 (and still occurring on some v7 builds) encodes NetFlow v9 sampling rate with the wrong byte order — so even when you configure 1:1000, the collector receives garbage and may interpret as 1:1 or random.
- During traffic spikes, some switches *increase* sample rate beyond the configured value, breaking calculations downstream.

**How to avoid:**
- **Pick sample rates based on smallest attack you want to detect**, not vendor defaults. For 1-10 Gbps ISP links, 1:512 or 1:1000 is usually a reasonable compromise.
- **Document the detection floor** explicitly: "with current sampling, attacks below X Mbps to a single /32 will not be detected reliably."
- **For Mikrotik**: set `netflow_ignore_sampling_rate_from_device enable` (the equivalent config) and **hard-code the sampling rate on the collector side**, not trusting what Mikrotik claims.
- **Per-protocol thresholds**: a 200 Mbps DNS-amplification has different signature than 200 Mbps of legitimate FTP; let small absolute thresholds work for known-attack-vector protocols (DNS, NTP, SSDP, memcached, CLDAP).
- **Aggregate detection at /24 and /22** in addition to /32 (see Pitfall 3 — carpet bombing).

**Warning signs:**
- Detected attack PPS doesn't match reality reported by customer / upstream.
- Flow counters in collector grossly under-count what link counters on the router show.
- Mikrotik flows arriving but PPS calculations look ~1000x off → sampling encoding bug.

**Phase to address:**
Phase: **Telemetry ingestion** (sampling-aware calculations, Mikrotik byte-order workaround, hard-coded fallback). Phase: **Detection engine** (multi-resolution aggregation: /32, /24, /22).

---

### Pitfall 3: Carpet-bombing evasion — per-IP thresholds blind to subnet-wide attacks

**What goes wrong:**
Attacker hits 1000 customer IPs in a /22 with 12 Mbps each. Total attack is 12 Gbps — link is saturated, customers complain — but no single IP crosses the per-IP threshold (let's say 100 Mbps). Detection engine sees nothing. RTBH never fires. ISP operator stares at green dashboards while phones ring. This is the #1 evasion technique against per-IP DDoS detection in 2024-2026.

**Why it happens:**
Detection logic written naively: "for each destination IP, check threshold." Attacker spreads load across an entire CIDR (often the customer's whole subnet) precisely to stay below the threshold while still saturating uplinks. Carpet-bombing has been the dominant evolution of volumetric attacks for 2-3 years.

**How to avoid:**
- **Aggregate detection at multiple prefix lengths**: /32, /28, /24, /22, /20 of each customer prefix. Cross-threshold at *any* level fires detection.
- **Total uplink utilization watchdog**: if uplink hits 95% but no per-IP threshold tripped → flag for human review.
- **Per-protocol total volume** across the announced address space (e.g., "total UDP/53 inbound > X Gbps across all my /20s" → likely DNS amplification, carpet-bombed).
- **Mitigation strategy difference**: carpet-bombing cannot be RTBH'd one /32 at a time (you'd blackhole the entire customer base). Must use Flowspec to drop the *attack pattern* (UDP source-port 53 from non-customer sources, or to specific dest-port ranges).

**Warning signs:**
- Uplink utilization spikes with no detection event.
- Customer support gets many simultaneous complaints from different customers with same upstream symptom.
- Flow data shows traffic to many distinct /32s in same /24, similar volume each, similar protocol.

**Phase to address:**
Phase: **Detection engine** (multi-resolution aggregation is foundational, not optional). Phase: **Mitigation engine** (Flowspec rules for pattern-based, not just /32 RTBH).

---

### Pitfall 4: BGP route leak — RTBH community escapes to upstream and blackholes others

**What goes wrong:**
Operator announces a /32 RTBH internally. Due to misconfigured outbound route-map (or community not stripped, or wrong well-known community 65535:666 used carelessly), the /32 leaks to upstream. Upstream accepts it because they accept /32 with blackhole communities from customers. Now traffic destined to that /32 is blackholed *across the wider internet*. If the /32 was not your address space, you just blackholed someone else's host (potentially across Tier-1 networks). See: Cloudflare 1.1.1.1 incident, June 27 2024 — AS267613 announced 1.1.1.1/32 with RTBH community, a Tier-1 honored it, blackholing 1.1.1.1 for that Tier-1's entire customer base.

**Why it happens:**
- Mitigation router shares the same eBGP session as production prefixes (instead of a dedicated mitigation peer).
- Outbound filter doesn't strip RTBH community before sending to non-RTBH-aware peers.
- Outbound prefix-list allows /32 announcements (it shouldn't, for non-blackhole peers).
- The /32 was an IP the operator does not own (typo, spoofed attack target, or detection of attack *sourced* from a third-party IP mistakenly treated as victim).

**How to avoid:**
- **Strict origin check before announcing**: the /32 must be inside an aggregate this ASN actually announces. Hard-fail if not.
- **Dedicated mitigation BGP session** (separate peer or separate session) with very restrictive policy: only /32s within owned space, only with the configured RTBH community, no other attributes.
- **Outbound filter on every other session**: strip blackhole community; reject /32 with blackhole community; reject any /32 to upstream unless explicit RTBH peer.
- **RPKI**: announce only RPKI-valid prefixes; mitigation /32s must validate against operator's ROA.
- **Audit log**: every BGP UPDATE the system sends, persisted with reason. Operator must be able to grep "why did we announce X.X.X.X/32 at time T?"

**Warning signs:**
- Upstream sends abuse complaint about /32 you don't own being blackholed.
- BGP looking-glass shows your AS originating prefixes you don't own.
- Customer outside your network reports they're unreachable from your transits.

**Phase to address:**
Phase: **Mitigation engine** (origin check, dedicated session, outbound filters as part of MVP — not "we'll add it later"). Phase: **Audit/logging** (every BGP UPDATE persisted with attribution).

---

### Pitfall 5: Detection lag — alert arrives after the link already fell

**What goes wrong:**
Attack starts at T+0s. Flow exporter aggregates with default 60-second active timeout. First flow packet leaves router at T+60s. Collector parses, runs detection (default 10s sliding window). Threshold tripped at T+70s. BGP announcement sent at T+70s, converges in 1-5s. Mitigation in effect at T+~75s. **Meanwhile**, the upstream link has been saturated for 75 seconds; the customer's voice calls dropped, video stalled, BGP session to upstream maybe flapped from congestion. The whole point of automation — sub-incident-detection — was defeated by configuration defaults. This is exactly what the PROJECT.md "incidente disparador" describes.

**Why it happens:**
- NetFlow v5/v9 default active timeout is 60s, inactive 15s. Most operators never change defaults.
- sFlow is real-time (datagram-per-sample) but if collector buffers/batches for "efficiency," same problem.
- Detection algorithms tuned for low false-positive rate use long windows (1-5 min) → too slow.
- BGP session to mitigation router has default keepalive 60s / hold 180s — congestion-induced session flap delays mitigation announcement.

**How to avoid:**
- **Set router active timeout to 1-10 seconds** for NetFlow/IPFIX, accepting higher flow volume. The MVP's bottleneck is detection speed, not storage.
- **Prefer sFlow when available** (real-time, no timeout) over NetFlow for the volumetric-detection path.
- **Short detection windows**: 5-10 seconds is reasonable for volumetric attacks; couple with multi-window confirmation to avoid FP (5s threshold AND 30s threshold both tripped → fire).
- **BFD on the mitigation BGP session** (sub-second failure detection) and aggressive BGP timers (keepalive 3s, hold 9s) on the *mitigation* session specifically — not on transit sessions.
- **Pre-computed mitigation announcement**: when threshold tripped, the BGP UPDATE is already constructed and ready to send (don't do template lookups in the hot path).
- **Measure and SLA detection-to-mitigation latency**: target < 15 seconds end-to-end. Display this on dashboard.

**Warning signs:**
- Time from first attack packet (on link counters) to RTBH announcement > 30s.
- Customer reports outage before dashboard shows detection.
- BGP session to mitigation router flapping during attacks.

**Phase to address:**
Phase: **Telemetry ingestion** (real-time sFlow path, short NetFlow timers documented for operators). Phase: **Detection engine** (short windows, confirmation logic). Phase: **Mitigation engine** (BFD, fast timers on mitigation peer).

---

### Pitfall 6: The mitigation tool itself becomes attack surface

**What goes wrong:**
The collector/controller sits between flow ingestion (UDP, often unfiltered from router IPs) and the BGP control plane of the ISP. Common scenarios:
- Web dashboard exposed to internet without auth (or with weak auth) — attacker logs in, triggers RTBH for ISP's own DNS resolver. Internal outage.
- Telegram bot token leaked in git history — attacker sends commands via bot.
- API endpoint that triggers mitigation requires no authentication ("internal only" but bound to 0.0.0.0).
- Collector parsing crafted sFlow/NetFlow packets from spoofed source IP → memory corruption / amplification of false positives.
- BGP daemon (exabgp/gobgp) reachable from any IP, password-less.
- Logs contain PII (customer IPs, traffic destinations) under LGPD scope — leak = legal liability.

**Why it happens:**
DDoS tooling is built by people thinking about *attack detection*, not *application security*. "It's an internal tool" is a common rationalization that ignores: insider threats, supply-chain compromises, lateral movement from an already-compromised internal host.

**How to avoid:**
- **All management interfaces bind to localhost or management VLAN only** by default; HTTPS+auth required even on internal interfaces.
- **No authentication-less endpoints, ever.** Including local-API endpoints (defense-in-depth).
- **Flow ingestion validates source IP** against a configured list of trusted exporters (your routers); drop unknown sources.
- **BGP session uses TCP-MD5 or TCP-AO authentication**; the BGP daemon binds only to mitigation-peer IPs.
- **Secrets management**: Telegram token, BGP password, DB credentials in env or vault; never in repo, never in logs.
- **Audit log of every mitigation action**: who/what triggered, source IP, time, RTBH/Flowspec content. Immutable, append-only.
- **Rate limit the API and bot commands**: an attacker who steals the token can't trigger 10k blackholes/sec.
- **Static analysis + dependency scanning** in CI (Trivy, dependabot).
- **Treat the tool as critical infrastructure**: SBOM, signed releases, reproducible builds.

**Warning signs:**
- Logs show flow datagrams from IPs not in router inventory.
- Dashboard accessible from public IP space (test with `curl` from outside).
- Telegram bot accepts commands from any chat ID, not allow-list.
- BGP daemon listening on 0.0.0.0:179.

**Phase to address:**
Phase: **Security hardening** (must be in MVP, not v2). Phase: **Operations/UI** (auth model from day one).

---

### Pitfall 7: No escape hatch — operator can't stop the system when it's wrong

**What goes wrong:**
Detection engine misfires during a real legitimate event (CDN deployment to ISP customers, a popular live stream, etc.). System mass-announces RTBH for many customers. Operator opens dashboard but UI doesn't have a "STOP EVERYTHING" button. Operator has to SSH to BGP daemon, find session, manually withdraw routes one by one — under pressure, with phone ringing, with sleep deprivation. Each minute of confusion = each minute of customers blackholed. This is the difference between "an outage" and "a career-ending incident."

**Why it happens:**
Builders think about happy path ("system detects attack, mitigates, withdraws"). Don't design for "system is wrong, get me out." Withdrawing mitigations is often slower / less tested than announcing them.

**How to avoid:**
- **Big red panic button** in UI: "Withdraw all active mitigations now." One click. Confirmation modal optional. Must work from mobile (operator is in bed).
- **Telegram command** `/panic_stop_all` with same effect, restricted to admin chat IDs.
- **Per-mitigation withdraw button**: any active mitigation listed has a 1-click withdraw.
- **CLI tool on the server**: `mitigador panic --withdraw-all` runs without DB / web stack (works even when those are broken).
- **Maintenance mode**: pause detection without losing telemetry — flag in config that disables auto-action but keeps logging.
- **Automatic max-mitigations safeguard**: refuse to announce more than N concurrent RTBHs without explicit operator confirmation (prevents detection-engine-runaway).
- **Test the panic path quarterly** with operator. If it's never been used in drill, it doesn't work.

**Warning signs:**
- Never any procedure / runbook for "system misfired, what now."
- Operator manually editing BGP daemon config to stop announcements — that's a missing feature, not a workflow.
- The withdraw-all path goes through the same DB/queue that's currently overloaded.

**Phase to address:**
Phase: **Operations/UI** (panic button, Telegram command, CLI tool all in MVP). Phase: **Mitigation engine** (max concurrent mitigations safeguard).

---

### Pitfall 8: Stuck blackhole — RTBH withdrawal silently fails, IP stays dead

**What goes wrong:**
Attack ends. Detection engine declares "all clear." System sends BGP WITHDRAW for the /32. But due to one of several real bugs:
- Mikrotik RouterOS 7 has a documented bug where the blackhole flag is **not removed when the community is withdrawn** — the route is gone from BGP but the FIB entry remains.
- BGP session flapped during the attack (congestion), the WITHDRAW message is lost, route stays in upstream RIB.
- ExaBGP/gobgp process crashed mid-cycle; restarts without re-announcing the still-active state, but also without explicit WITHDRAW.
- Operator manually approved RTBH but then forgot to revoke it; no automatic timeout.

Customer remains offline for hours or days after attack ended. ISP discovers when customer calls. Embarrassing and customer-trust-destroying.

**Why it happens:**
- BGP UPDATEs are not transactional / acknowledged at the application layer. "Sent" ≠ "applied at upstream."
- No verification feedback loop: after withdrawing, no check that traffic actually flows again.
- No mandatory expiration on mitigation: "announce forever until withdrawn" is the default assumption.

**How to avoid:**
- **Mandatory expiration on every mitigation**: announce with a TTL (e.g., 30 min). After TTL, auto-withdraw. To extend, re-announce. This is the *single most important safety* against stuck blackholes.
- **Post-withdrawal verification**: after withdraw, ping the IP / check if traffic resumes within N seconds. If not, escalate alert.
- **Mikrotik-specific workaround**: for any Mikrotik routers in path, periodically refresh BGP session or explicitly send route-refresh after withdrawal until the bug is fixed.
- **State reconciliation on startup**: when the BGP daemon (or controller) restarts, re-sync intended mitigations with announced state; resolve drift.
- **Heartbeat**: detection engine continuously asserts "these mitigations should be active"; mitigation engine periodically reconciles.
- **Operator alert** on mitigation > N minutes old, asking "still needed?"

**Warning signs:**
- Customer complains after attack already ended ("I'm still offline").
- BGP table on upstream shows /32 with no corresponding state in your detection engine.
- Number of "active mitigations" in dashboard grows monotonically over weeks.

**Phase to address:**
Phase: **Mitigation engine** (TTL on every announcement, state reconciliation, Mikrotik refresh workaround). Phase: **Operations/UI** (stale-mitigation alerts).

---

### Pitfall 9: Storage explosion — keeping raw flows costs TB/month nobody budgeted for

**What goes wrong:**
A medium ISP with a few CCR-class routers, 1:1000 sFlow + NetFlow on PE, can easily generate 5000-50000 flows/sec. At ~150 bytes/flow record, that's 65-650 GB/day of raw data, or 2-20 TB/month. Operator expected "a few GB" because they saw a demo with one router. Three weeks in, disk full. Detection breaks. Or, operator pre-deletes old data, losing post-mortem evidence right when needed.

**Why it happens:**
- Raw flow data isn't the same as the dashboard metrics; people conflate them.
- Demo / lab environments use 1-2 routers; production has 5-20.
- ELK / Clickhouse / InfluxDB without retention policy → grows unbounded.
- No aggregation strategy from day one; "we'll add it when we need it" = adding it under pressure.

**How to avoid:**
- **Tiered retention from day one**:
  - Raw flows: 24-72 hours (forensic window for the most recent incident).
  - Per-minute aggregates: 30 days (dashboard, trend analysis).
  - Per-hour aggregates: 1 year (capacity planning, baselining).
- **Choose a time-series store with downsampling built in** (Clickhouse with materialized views, TimescaleDB hypertables, VictoriaMetrics, Prometheus + Thanos with downsampling). Avoid raw Postgres for time-series.
- **Compress raw flow records**: protobuf or columnar formats, gzip/zstd.
- **Document expected disk usage**: "for N routers at X flows/sec, you need Y GB/day raw + Z GB aggregate."
- **Operator-facing storage dashboard**: show projected fill date, configurable alarms.
- **Don't store full payloads** — only headers/aggregates from sampled flows; no PCAP.

**Warning signs:**
- Disk usage growing linearly with no cap.
- Query latency increasing month over month.
- Operator manually `rm`-ing old data.

**Phase to address:**
Phase: **Telemetry ingestion / storage** (tiered retention designed in, not bolted on). Phase: **Operations/UI** (storage health visible).

---

### Pitfall 10: Brazilian legal/regulatory — Marco Civil + LGPD compliance for traffic data

**What goes wrong:**
Marco Civil da Internet (Lei 12.965/2014) requires ISPs to retain *connection logs* for 1 year and *application logs* (for application providers) for 6 months — but **also restricts what can be retained**, especially regarding content. Flow records (5-tuple + bytes) sit in an ambiguous zone: arguably "connection metadata" needed for legitimate security purposes, but they identify customer IPs and contacted destinations.

LGPD (Lei 13.709/2018) treats IP addresses (when linkable to a person — and an ISP's CGNAT/PPPoE logs *make them linkable*) as personal data. Storing/processing it requires lawful basis (legitimate interest works for DDoS defense, but must be documented), data subject rights (access, deletion), and ANPD-style accountability.

Blackholing a *customer's* /32 without warning/consent could be challenged as service disruption, especially for SLA'd corporate clients. "We blackholed you for your own good" is not always an accepted defense if the customer was actually under a manageable attack and would have preferred degraded service to no service.

**Why it happens:**
Builders are technical, not legal. Brazilian ISP operators sometimes assume "we own the network, we can do what we want" — but contracts and law disagree.

**How to avoid:**
- **Document the lawful basis** for processing flow data (LGPD Art. 7 VI — legitimate interest of the controller; security; protection of the data subject). Have a `legal-basis.md` in the repo.
- **Data minimization**: don't store more than needed for detection/forensic. No payloads, just metadata. Aggregate aggressively.
- **Retention limits aligned to Marco Civil**: raw flows ≤ 6 months (much shorter is better — 24-72h is enough for detection). Don't accidentally become a year-long surveillance archive.
- **Customer terms must cover mitigation**: the ISP's service contract should explicitly authorize automated DDoS mitigation including potential blackholing. Without this clause, an angry customer has a contract-law claim.
- **Audit log of customer-impacting actions** (every RTBH on a customer prefix): with timestamp, reason, evidence, automatic vs manual. Needed for both LGPD accountability and contract disputes.
- **Opt-out mechanism for corporate customers**: SLA'd clients should be able to disable automatic mitigation on their prefixes ("call me first"), with a documented procedure.
- **DPO contact in UI/docs**: as a tool processing personal data on behalf of an ISP, downstream ISP needs to be able to demonstrate LGPD compliance.

**Warning signs:**
- Flow data retained > 6 months "just in case."
- No mention of mitigation in ISP service contracts.
- Customer complaints reach Procon / ANATEL with no audit trail to respond.

**Phase to address:**
Phase: **Audit/logging** (immutable audit log of customer-impacting actions, in MVP). Phase: **Operations/UI** (customer opt-out config, DPO/contact docs). Phase: **Storage** (retention enforcement, not just "policy on paper").

---

## Moderate Pitfalls

### Pitfall 11: Multi-tenant misconfiguration crossover

**What goes wrong:**
PROJECT.md says ISP and corporate-customer get *separate installs*. Good — but if a single config template / Ansible role is used across both, a copy-paste of ASN/community accidentally makes the corporate-customer instance announce with the ISP's community, triggering RTBH on the wrong ASN.

**How to avoid:**
- Each install has a `tenant.yml` with its ASN, communities, allowed prefix list — and the system **refuses to start** if those don't match what BGP peer expects.
- Config validation at startup: announce a test prefix to mitigation peer, verify acceptance with expected community echo.

**Phase to address:** Phase: **Mitigation engine** (config validation at startup, in MVP).

---

### Pitfall 12: Alert storm / Telegram rate-limit

**What goes wrong:**
A carpet-bombing or large attack triggers 100 separate detections in 1 second. Bot tries to send 100 Telegram messages. Telegram API rate-limits at 30 messages/sec per bot; messages queue, arrive minutes late, operator misses the first (most important) alert in the flood. Or, Telegram bot gets banned for spam.

**How to avoid:**
- **Alert aggregation window**: collapse multiple detections within N seconds into one summary message ("12 IPs in 192.0.2.0/24 attacked, total 8 Gbps UDP/53, mitigations active").
- **Severity-based throttling**: HIGH severity always sends; MEDIUM aggregates; LOW summarized hourly.
- **Per-channel deduplication**: same attack on same IP doesn't re-alert every 30s.
- **Use a queue with bounded size**; drop low-severity if overwhelmed, never drop HIGH.

**Phase to address:** Phase: **Alerting** (aggregation + dedup + rate limiting, in MVP).

---

### Pitfall 13: Mikrotik sFlow / NetFlow CPU exhaustion

**What goes wrong:**
Enabling NetFlow on a CCR1009 / CCR2004 under heavy load pushes CPU to 100%; router degrades, packet loss appears that wasn't there before mitigation tooling was installed. Customers blame "the new DDoS tool" — and they're right. RouterOS Traffic-Flow only processes traffic that hits CPU; hardware-offloaded traffic (fast-path) isn't seen anyway.

**How to avoid:**
- **Document supported Mikrotik models and load levels**; warn operators on under-spec hardware (CCR1009 at >70% link utilization is risky).
- **Recommend sFlow > NetFlow on Mikrotik** when both available — generally lighter, and sample-rate-native.
- **Prefer per-port sampling** over global; only sample uplinks where attacks land.
- **Test in lab with synthetic load before production**.

**Phase to address:** Phase: **Telemetry ingestion** (docs and supported-config matrix). Phase: **Operations** (per-router health check showing CPU before/after enabling flow export).

---

### Pitfall 14: Flowspec rejected by upstream / unsupported by peer

**What goes wrong:**
Operator configures Flowspec mitigation. RTBH still works (well-known community 65535:666). But Flowspec is much less universally supported — many Brazilian Tier-2 upstreams don't accept Flowspec from customers, or only accept specific match types. System announces Flowspec, peer silently ignores or rejects, attack continues.

**How to avoid:**
- **Probe at startup**: send a no-op Flowspec rule, verify acceptance. If rejected, mark Flowspec mitigation as unavailable in UI.
- **Fall back to RTBH** automatically if Flowspec announce fails or the attack pattern can't be expressed as Flowspec (carpet-bombing → must use Flowspec; if peer doesn't support, escalate to operator).
- **Document supported peer matrix**: which Brazilian upstreams accept Flowspec, with which restrictions. Maintain this as community-contributed list.
- **Follow RFC 9117 validation rules** (revised Flowspec validation) when announcing — don't rely only on RFC 5575 (older, more permissive, less safe).

**Phase to address:** Phase: **Mitigation engine** (Flowspec capability detection at startup, fallback logic).

---

### Pitfall 15: NetFlow template loss after exporter restart

**What goes wrong:**
Router (NetFlow v9 or IPFIX exporter) restarts. Templates are re-sent at intervals; collector that started receiving data *between* template sends can't decode flows. Detection appears to work fine on most flows (those of templates it has), silently drops the rest. Coverage gaps invisible.

**How to avoid:**
- **Set router template-refresh interval to 60 seconds or less** (default is often 30 min on Cisco — way too long).
- **Collector tracks template-known-vs-flows-received ratio**; alert when many flows arrive for unknown templates.
- **Persist templates** across collector restarts (don't lose them between collector reboots).

**Phase to address:** Phase: **Telemetry ingestion** (template persistence, template-coverage health metric).

---

## Minor Pitfalls

### Pitfall 16: Hardcoded thresholds in Mbps, ignoring IPv6 / large MTU

**What goes wrong:** Threshold expressed only in pps assumes typical 1500-byte packets. Jumbo-frame customers (9000 MTU) hit pps limits much earlier in legitimate use. Or, IPv4-only threshold misses IPv6 attacks entirely.

**How to avoid:** Thresholds in both bps AND pps; IPv4 and IPv6 separately; document defaults explicitly.

**Phase to address:** Phase: **Detection engine**.

---

### Pitfall 17: Timezone bugs in baselining

**What goes wrong:** Baseline learned in UTC, operator reads dashboard in America/Sao_Paulo, "peak hours" mismatch makes anomaly detection wrong by 3 hours.

**How to avoid:** Store in UTC, display in operator-configured tz, baseline by *local* time-of-day (people use the internet on local schedules).

**Phase to address:** Phase: **Detection engine** + **UI**.

---

### Pitfall 18: Forgetting IPv6

**What goes wrong:** Mitigation only handles IPv4; IPv6 attacks pass uninhibited. Brazilian ISPs are increasingly dual-stack; v6 attacks growing.

**How to avoid:** Detect and mitigate both address families from day one. Separate Flowspec AFI/SAFI session if peer requires.

**Phase to address:** Phase: **All phases**.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Static global threshold | Ship MVP faster | False positives → operator distrust → tool gets disabled | Only if dry-run mode is the *only* mode in MVP |
| Single all-purpose BGP session (transit + mitigation) | One config | Route leak risk to upstream | Never — separate sessions are non-negotiable |
| In-memory only state (no persistence) | Simpler code | Lost mitigations on restart → stuck blackholes | Never for active mitigations; OK for sampled flow buffer |
| No audit log | Skip a table/schema | Can't defend against LGPD complaint or contract dispute | Never |
| Hardcoded router IPs in config | Quick start | Painful when topology changes; CSRF-able if API exposes | OK for first 1-2 router prototype; mandatory inventory module by production |
| Skip dry-run mode | Earlier "real" usefulness | First false-positive incident kills trust irreversibly | Never |
| Trust Mikrotik-reported sampling rate | One less config option | Silent 1000x detection error | Never for Mikrotik — always hardcode |
| Single threshold (per-IP only) | Simpler detection logic | Misses carpet-bombing (dominant attack 2024-2026) | Never for ISP context |

---

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Mikrotik NetFlow v9 | Trust the sampling rate the router announces | Hardcode sampling rate on collector; set `netflow_ignore_sampling_rate_from_device` |
| Mikrotik RTBH withdrawal | Assume route gone = blackhole flag gone | Test withdrawal; use route-refresh; track upstream BGP table state |
| Juniper Flowspec | Assume validation rules are RFC 5575 | Implement RFC 9117 validation; test with Juniper's strict validator |
| Cisco IOS NetFlow v5 | Assume 32-bit sequence numbers don't wrap | Handle wraparound in sequence-gap detection |
| ExaBGP | Run as same process as web UI | Separate processes; ExaBGP has its own state machine and crash modes |
| GoBGP | Skip TCP-MD5 because "it's internal" | Always use auth; mitigation peer is too privileged |
| Telegram bot | Send messages in tight loop | Respect 30 msg/sec; aggregate; backoff on 429 |
| sFlow source IP | Accept from any source | Validate source IP matches configured router inventory |
| BGP upstream | Announce /32 RTBH to all peers | Dedicated RTBH peer; strip community on other peers; filter on origin |
| NetFlow exporter | Use UDP from 0.0.0.0 source | Set explicit source IP; Linux drops martian packets |

---

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Per-flow database insert | Insert latency grows linearly | Batch + columnar store (Clickhouse) or time-series (Victoria/Timescale) | At ~1-5k flows/sec sustained |
| Single-threaded flow parser | CPU pinned at 100% on one core, drops on UDP socket | Workers per-router or per-port; lock-free queues | At ~10-50k pps of sampled flow |
| Naive sliding-window in RAM | Memory grows with traffic diversity | Sketches (Count-Min, HyperLogLog) for cardinality-heavy stats | At medium ISP scale (~/22 of customer prefixes) |
| Polling BGP table for state | Lag, missed transient state | Subscribe to BGP UPDATEs (gobgp gRPC, ExaBGP api) | Always — but worse at scale |
| Synchronous Telegram send in detection path | Detection blocked by network I/O | Async queue; detection never blocks on alerting | Always — but invisible until first alert storm |
| Storing every sampled packet | Disk fills | Aggregate at minute granularity; retain raw only for short window | Within weeks |
| Re-computing baseline from scratch | CPU spike daily | Incremental statistics; EWMA / streaming algorithms | At a few weeks of data |

---

## Security Mistakes

| Mistake | How Users Suffer | What to Do Instead |
|---------|------------------|-------------------|
| Web UI bound to 0.0.0.0 with weak/no auth | External attacker triggers mass blackhole | Bind to mgmt VLAN; mandatory strong auth; 2FA for destructive actions |
| BGP daemon without MD5/AO | Attacker spoofs RTBH announcements to your mitigation router | TCP-MD5 minimum; TCP-AO if both peers support |
| Telegram bot token in repo or logs | Token leaks → adversary controls mitigation | Vault/env; rotate on suspicion; allow-list of authorized chat IDs |
| Flow exporter not source-validated | Spoofed flow datagrams trigger false RTBH on chosen victim | Validate source IP; consider IPsec or TLS-encapsulated flow (where supported) |
| API auth tokens never expire | Old credentials of departed staff still work | Short-lived tokens; quarterly rotation; audit access |
| No rate limit on mitigation API | One bug = thousands of blackholes | Hard cap on mitigations/minute; require confirmation above threshold |
| Storing customer IP in logs without retention limit | LGPD violation, fine + reputational | Retention policy enforced (cron job, not just docs) |
| Dependency vulnerabilities ignored | Supply-chain compromise of critical infrastructure | Trivy/Dependabot in CI; quarterly review |

---

## "Looks Done But Isn't" Checklist

- [ ] **RTBH mitigation:** Announces correctly — but does it **withdraw correctly under all conditions** (including Mikrotik bug, BGP session flap, daemon restart)? Verify by full attack-mitigation-end cycle in lab.
- [ ] **Detection engine:** Works for the test attack scenario — but does it **catch carpet-bombing** (subnet-distributed)? Test with attack tool that targets a /24, not a single IP.
- [ ] **Dashboard:** Shows attacks — but does it have a **panic button** that works from mobile during a real-time crisis? Test from phone, in the dark, half-asleep.
- [ ] **Alerting:** Telegram works — but does it survive an **alert storm** (100 detections/sec) without spamming or dropping the most important alert? Stress-test.
- [ ] **Flow ingestion:** Receives flows — but does **sampling-rate math** produce correct PPS/BPS on Mikrotik exporters? Cross-check with router's own counters.
- [ ] **BGP mitigation:** Announces — but is the **outbound filter on transit sessions** stripping the RTBH community and rejecting /32? Test by trying to leak.
- [ ] **Multi-tenant:** Two instances run — but do they **refuse to start** with mismatched ASN/community/peer config? Test with deliberately wrong config.
- [ ] **Authentication:** Login screen exists — but is the **API rate-limited**, are bot commands **allow-listed by chat ID**, are tokens **stored encrypted**? Audit.
- [ ] **Audit log:** Logs exist — but are they **immutable**, queryable by `who/what/when/why`, retained per LGPD policy? Verify schema and access control.
- [ ] **Storage:** Database works — but is **tiered retention enforced**, with raw flow data aged out automatically? Watch for 30 days.
- [ ] **Manual override:** Operator can approve — but can they **also reject and have rejection remembered** (don't re-prompt same false positive every 30 seconds)? Test.
- [ ] **Mitigation expiration:** TTL exists — but does the system **actually withdraw at TTL** even if detection-engine state is lost? Verify by killing detection mid-mitigation.

---

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| False-positive blackholed legit customer | MEDIUM (minutes; reputational hours-days) | Panic button → withdraw all OR specific /32; notify customer; post-mortem; tune threshold; add to VIP list |
| Stuck blackhole (Mikrotik bug) | MEDIUM (minutes if detected) | Force BGP route-refresh on Mikrotik; if persists, manual `/ip route remove` on router; file bug |
| Route leak to upstream | HIGH (hours; trust damage) | Immediately withdraw; contact upstream NOC; review outbound filters; RPKI ROAs; audit-log review |
| Detection engine crash mid-attack | HIGH (attack continues unmitigated) | Manual mitigation via CLI/Telegram; restart engine; reconcile state; root-cause |
| Alert storm masked real critical alert | MEDIUM (delayed response; not loss-of-control) | Implement aggregation; review which alert was missed; add to runbook |
| Storage full | HIGH (telemetry loss = detection loss) | Emergency: drop oldest raw flows; resize disk; reconfigure retention; rebuild aggregates if possible |
| Carpet-bombing not detected | HIGH (link saturated, customers offline) | Manual Flowspec announcement targeting attack pattern; add multi-resolution detection; post-mortem |
| Management plane compromise | CRITICAL (treat as full incident) | Rotate all credentials; disable web UI; offline forensic on collector; rebuild from clean image |
| LGPD data subject request | LOW (process) | Documented procedure; query audit log; deletion respects retention obligations |
| Operator-induced mass blackhole (typo) | MEDIUM (minutes) | Panic button; revoke recent N mitigations; add confirmation modals; per-action max-impact limit |

---

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| #1 False-positive blackhole | Mitigation engine + Ops/UI | Dry-run default; manual-approve mode; VIP list; lab test with legitimate spike |
| #2 Sampling-rate blindness | Telemetry ingestion | Mikrotik byte-order fix; sample-rate config documented; PPS sanity-check vs router counters |
| #3 Carpet-bombing evasion | Detection engine | Multi-resolution (/32, /28, /24, /22) aggregation; lab test with subnet attack tool |
| #4 BGP route leak | Mitigation engine | Dedicated mitigation peer; outbound filter on transit peers; origin check; RPKI ROAs; leak test in lab |
| #5 Detection lag | Telemetry + Detection + Mitigation | Measured end-to-end < 15s in lab; sFlow real-time path; BFD on mitigation peer |
| #6 Tool as attack surface | Security hardening | Bind to mgmt VLAN; auth required; secrets in vault; pen-test before production |
| #7 No escape hatch | Operations/UI | Panic button in web + Telegram + CLI; tested quarterly; max-mitigations safeguard |
| #8 Stuck blackhole | Mitigation engine | Mandatory TTL; state reconciliation; Mikrotik route-refresh workaround; post-withdraw verification |
| #9 Storage explosion | Telemetry/Storage | Tiered retention from MVP; storage projections in UI; raw-flow window ≤ 72h |
| #10 LGPD / Marco Civil | Audit/logging + Storage | Immutable audit log; retention enforcement; contract clause docs; opt-out for SLA customers |
| #11 Multi-tenant crossover | Mitigation engine | Tenant config validation at startup; refuse mismatched ASN/community |
| #12 Alert storm | Alerting | Aggregation window; dedup; severity-based throttling; Telegram rate-limit handling |
| #13 Mikrotik CPU exhaustion | Telemetry + Ops | Supported-hardware matrix; per-router CPU health metric; pre-deploy lab test |
| #14 Flowspec unsupported | Mitigation engine | Capability probe at startup; RTBH fallback; peer matrix documented |
| #15 NetFlow template loss | Telemetry ingestion | Template persistence across restarts; template-coverage metric; recommend 60s refresh |
| #16 IPv6 / MTU thresholds | Detection engine | Thresholds in bps AND pps; v4 + v6 separately |
| #17 Timezone bugs | Detection engine + UI | UTC storage; local-time baselining; tz config |
| #18 IPv6 missed | All phases | Dual-stack from day one; separate AFI/SAFI BGP if needed |

---

## Sources

- [FastNetMon — When is BGP blackholing a good choice (and when isn't it?)](https://fastnetmon.com/2025/06/18/when-is-bgp-blackholing-a-good-choice-for-ddos-mitigation-and-when-is-it-not/)
- [FastNetMon — BGP Blackhole automation for DDoS mitigation (2026)](https://fastnetmon.com/2026/01/06/bgp-blackhole-automation-for-ddos-mitigation/)
- [FastNetMon — How to set a threshold for RTBH/BGP Blackhole](https://fastnetmon.com/2025/07/02/how-to-set-a-threshold-for-rtbh-bgp-blackhole-a-practical-guide-to-threshold-based-ddos-defence/)
- [FastNetMon — Mikrotik configuration and known issues](https://fastnetmon.com/docs-fnm-advanced/mikrotik/)
- [FastNetMon issue #985 — RouterOS v6.49.6 wrong byte order in NetFlow v9 sampling rate](https://github.com/pavel-odintsov/fastnetmon/issues/985)
- [MikroTik forum — RTBH blackhole flag not removed when community withdrawn (RouterOS 7 bug)](https://forum.mikrotik.com/t/bgp-rtbh-blackhole-flag-not-removed-when-community-is-withdrawn/181566)
- [MikroTik Traffic Flow docs (CPU-only processing limitation)](https://help.mikrotik.com/docs/spaces/ROS/pages/21102653/Traffic+Flow)
- [Cloudflare 1.1.1.1 incident — RTBH /32 leak by AS267613 (June 2024)](https://blog.cloudflare.com/cloudflare-1111-incident-on-june-27-2024/)
- [Cloudflare route leak incident, January 22 2026 (IPv6 export policy too permissive)](https://blog.cloudflare.com/route-leak-incident-january-22-2026/)
- [RFC 9117 — Revised Validation Procedure for BGP Flow Specifications](https://datatracker.ietf.org/doc/rfc9117/)
- [M3AAWG — BGP Flowspec Best Practices](https://www.m3aawg.org/flowspec-BP)
- [NETSCOUT — Carpet-bombing DDoS attacks](https://www.netscout.com/blog/asert/carpet-bombing)
- [A10 Networks — Real-time DDoS carpet-bombing evading per-IP defenses](https://www.a10networks.com/blog/carpet-bombing-ddos-the-attack-pattern-your-per-ip-defenses-wont-catch/)
- [Nokia — Do baselines and thresholds work to protect unpredictable IP networks](https://www.nokia.com/blog/do-baselines-and-thresholds-work-to-protect-critical-unpredictable-ip-networks-from-ddos-attacks/)
- [Kentik — DDoS Protection and Mitigation: A 2026 Guide](https://www.kentik.com/kentipedia/ddos-protection/)
- [Cisco — Remotely Triggered Black Hole Filtering (operator's reference)](https://www.cisco.com/c/dam/en_us/about/security/intelligence/blackhole.pdf)
- [NSRC — RTBH workshop materials](https://nsrc.org/workshops/2025/nsrc-pacnog36-pcio/networking/routing-security/en/presentations/RTBH.pdf)
- [ExaBGP wiki — Production Best Practices](https://github.com/Exa-Networks/exabgp/wiki/Production-Best-Practices)
- [ExaBGP wiki — DDoS Mitigation](https://github.com/Exa-Networks/exabgp/wiki/DDoS-Mitigation)
- [APNIC — Reflecting on 10 years in open-source network security (FastNetMon retrospective)](https://blog.apnic.net/2025/08/20/reflecting-on-10-years-in-open-source-network-security-fastnetmon/)
- [OyuAI — NetFlow deduplication and storage volume problem](https://oyu.ai/2026/01/23/stop-paying-for-duplicate-data-how-flow-deduplication-reduces-netflow-storage-costs-by-90/)
- [Noction — NetFlow vs sFlow vs IPFIX (UDP/template caveats)](https://www.noction.com/blog/netflow-sflow-ipfix-netstream)
- [Brazil LGPD — overview and ANPD enforcement](https://iclg.com/practice-areas/data-protection-laws-and-regulations/brazil)
- [Flowtriq — FastNetMon vs Wanguard vs Flowtriq comparison (mitigation safeguards)](https://flowtriq.com/blog/fastnetmon-vs-wanguard-vs-flowtriq)
- [Jaze Networks — BGP Flowspec for DDoS mitigation (real-time cycle ≤ 30s)](https://www.jazenetworks.com/tech-news/bgp-flowspec-for-ddos-mitigation-how-isps-can-block-attacks-in-real-time/)
- [ipSpace.net — BGP Convergence Optimization](https://www.ipspace.net/BGP_Convergence_Optimization)

---
*Pitfalls research for: DDoS mitigation platform for small/medium Brazilian ISPs (Mitigador)*
*Researched: 2026-05-17*
