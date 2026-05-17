# Feature Research

**Domain:** DDoS detection and mitigation platform for ISPs (volumetric attacks, BGP-based response)
**Researched:** 2026-05-17
**Confidence:** HIGH (commercial platforms documented), MEDIUM (Brazilian ISP norms inferred from community sources)

---

## Feature Landscape

### Table Stakes (Users Expect These)

These are non-negotiable for ISP operators. Missing any of them = the product is not viable as a serious DDoS platform.

#### Detection — Telemetry Ingestion

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **sFlow ingestion (v5)** | Dominant in Mikrotik (RouterOS), most ISP edge gear in BR | MEDIUM | UDP listener, decoder; libraries exist (goflow2, fastnetmon-style). Sampling-rate aware aggregation is the real work. |
| **NetFlow v5/v9 ingestion** | Cisco/legacy installed base; v9 has template flexibility | MEDIUM | v9 templates must be cached per exporter; v5 is fixed-format and simpler. |
| **IPFIX ingestion** | Modern Juniper, Cisco; IETF standard | MEDIUM | Variant of v9 with vendor-specific element IDs (e.g., Juniper enterprise IEs). |
| **Per-host / per-prefix traffic accounting** | Operators need to know *which* IP is being hit, not just "traffic is up" | MEDIUM | Requires keeping a rolling counter per /32 (or per /24); memory-bounded with eviction. |
| **Bandwidth and PPS metrics simultaneously** | Volumetric is bps; packet-rate floods (SYN/small UDP) need pps | LOW | Both are computed from the same flow records; cheap to add once you have one. |

#### Detection — Attack Identification

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Threshold-based detection** | The baseline mechanism every platform ships with | LOW | Per-host bps/pps/flow thresholds. Triggers when sustained over N seconds. |
| **Per-host / per-network thresholds** | Single global threshold is useless when a residential /32 and a corporate /29 share the network | MEDIUM | FastNetMon calls these "hostgroups"; Wanguard "subnet groups". Required for tunable false-positive rate. |
| **Direction-aware thresholds** | Inbound flood ≠ outbound abuse (compromised customer); need different limits | MEDIUM | FastNetMon Advanced added `per_direction_hostgroup_thresholds` in 2025 specifically for this. |
| **Attack vector classification** | "UDP flood on port 53 from 800 sources" is actionable; "high traffic" is not | MEDIUM | Classify by L4 protocol, dst port, source diversity. Common vectors: UDP flood, ICMP flood, DNS/NTP/SSDP/Memcached amplification, SYN flood. |
| **Sub-minute detection latency** | If detection takes 2 minutes the link is already down; operators expect seconds | HIGH | Drives entire architecture (streaming aggregation vs batch). Detection within 5–10s is the bar. |

#### Mitigation — BGP Response

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **BGP RTBH announcement (/32 blackhole)** | The single most universally supported DDoS mitigation in ISP networks | MEDIUM | Speak BGP via gobgp/exabgp; advertise /32 with blackhole community (e.g., upstream's RFC 7999 65535:666 or operator-specific). |
| **BGP Flowspec announcement** | Granular mitigation that doesn't blackhole the customer entirely | HIGH | Flowspec NLRI encoding is non-trivial; Mikrotik Flowspec support is limited/recent, Juniper/Cisco mature. gobgp handles encoding. |
| **Configurable BGP communities per upstream** | Each carrier (Telxius, Lumen, Algar, etc.) has its own blackhole community | LOW | Just config; but the absence of this kills RTBH usability. |
| **Automatic mitigation withdrawal after attack ends** | Operators won't leave a /32 blackholed forever; must auto-unblock when traffic drops | MEDIUM | Hold-down timer + post-mitigation observation window. |
| **Mitigation duration / TTL configuration** | Operator-tunable; some ISPs want 1h hold, others 24h | LOW | Simple config knob, but critical for tuning. |

#### Alerting

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Telegram alerts** | De facto standard for Brazilian ISP NOC notifications; every blog/MUM presentation uses it | LOW | Bot API is trivial; message formatting (attack IP, vector, pps/bps) is the real design work. |
| **Email alerts (SMTP)** | Audit trail, ticket creation upstream, non-Telegram users | LOW | SMTP client; HTML formatting optional. |
| **Real-time dashboard alert** | Operator looking at NOC screen needs immediate visual | LOW | WebSocket or SSE push to dashboard. |
| **Alert deduplication** | One attack ≠ 50 Telegram messages every 5 seconds | MEDIUM | Per-target deduplication key with cooldown; PagerDuty's `dedup_key` pattern is the reference. |
| **Alert payload with attack details** | "DDoS detected" is useless; need target IP, vector, pps/bps, top source ASNs, action taken | LOW | Templating concern; ensure all fields populated. |

#### Reporting / Forensics

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Attack history / log** | Operator must review what happened last week to justify thresholds and explain outages | MEDIUM | Persistent storage (ClickHouse or TimescaleDB are domain-standard for flow data). |
| **Per-attack summary view** | Start/end time, target, vector, peak pps/bps, sources, mitigation action | LOW | Detail page per incident; once data is stored, rendering is trivial. |
| **Top talkers during attack** | "Which source ASNs / countries / IPs sent the most?" — needed for ISP abuse reports | MEDIUM | Aggregate flow records during attack window; common output of every commercial tool. |
| **Time-series traffic graphs** | The basic "is the network okay" view every NOC has | LOW | Grafana with InfluxDB/Prometheus/ClickHouse is standard pattern. |

#### Multi-Tenancy / Configuration

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Multi-tenant via separate install** (per PROJECT.md scope) | ISP runs one, corporate customer runs another — independent ASNs, BGP peers, thresholds | LOW | If "separate install" is the model, this is mostly a packaging/deployment concern, not an app concern. |
| **Authenticated web UI** | Even single-tenant installs need login (NOC has multiple operators) | LOW | Standard session/JWT auth; not 50-role RBAC. |
| **Configuration via UI (not just files)** | NOC operators won't edit YAML at 3am during an attack | MEDIUM | CRUD for hostgroups, thresholds, BGP peers, alert channels. |

#### Operational

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Audit log of mitigation actions** | "Why was customer X blackholed?" is a recurring forensic question | LOW | Append-only log table; record actor (system vs operator), target, reason, threshold breached. |
| **Configuration backup / export** | NOC needs to restore config when re-deploying | LOW | YAML/JSON export endpoint. |
| **Health endpoint** | Monitoring system needs to know if Mitigador itself is up | LOW | `/health` returning ingestion lag, BGP session state, last alert sent. |
| **BGP session state visibility** | If the BGP session to the router is down, no mitigation works — operator must see this | LOW | Surface gobgp/exabgp state in dashboard. |

---

### Differentiators (Competitive Advantage)

Features that distinguish Mitigador from FastNetMon Community / lupael / rolled-your-own scripts.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Brazilian ISP defaults out of the box** | Pre-loaded BGP communities for Lumen/Telxius/Algar/Ascenty/V.tal, Telegram-first onboarding, Portuguese UI | LOW | Pure config + i18n work; massive UX win in the target market. FastNetMon/Wanguard are English-first. |
| **Mikrotik-native first-class support** | Specific docs/templates for Mikrotik BGP session config, RouterOS scripting examples, sFlow setup helpers | MEDIUM | Mikrotik dominates BR ISP segment; commercial platforms treat it as second-class behind Juniper/Cisco. |
| **Manual approval / "dry-run" mode** | Before automating blackhole, operator can review proposed actions in Telegram with one-tap approve/reject | MEDIUM | Critical for trust-building. Telegram inline keyboard makes this elegant. Mitigates "false positive blackholes customer" nightmare. |
| **Baseline learning mode** | Run for 1–2 weeks collecting peak traffic per host before recommending thresholds | MEDIUM | FastNetMon Advanced has this; differentiates from naive threshold tools. Auto-suggests thresholds based on observed peaks + safety margin. |
| **Attack timeline with one-click PCAP-style summary** | Even without full PCAP capture, show "this attack peaked at 8.2Gbps UDP/53 from these 47 source ASNs" — enough for an abuse report | MEDIUM | Aggregate flow records during the attack window; no need for full packet capture (which is anti-feature for MVP — see below). |
| **Telegram bot with rich controls** | Not just notifications: `/status`, `/active`, `/unblock <ip>`, `/threshold <prefix>`, inline approval buttons | MEDIUM | Operators are on mobile during incidents; Telegram-as-CLI is a force multiplier in BR market. |
| **Per-prefix threshold templates** | "Residential /32", "Corporate /29", "Gaming server", "DNS server" — one-click templates with sane defaults | LOW | Pure UX/config layer over the threshold engine. Huge ergonomics win. |
| **Native heterogeneous flow ingestion** | Accept sFlow + NetFlow + IPFIX simultaneously on the same install (one ISP, multiple router vendors) | MEDIUM | FastNetMon and Wanguard do this; lupael/scripts often don't. Table-stakes-becoming-differentiator vs DIY. |
| **Whitelist / trusted-networks list** | Never blackhole CDN IPs, monitoring sources, NOC office, etc. | LOW | Critical safety rail; easy to implement, prevents catastrophic mistakes. |
| **Post-mitigation observation + auto-unblock** | After blackhole is applied, observe traffic. When attack subsides, automatically withdraw the route. | MEDIUM | Operators forget to unblock; this saves customers from staying offline for hours. |

---

### Anti-Features (Commonly Requested, Often Problematic — Do NOT Build)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| **Built-in scrubbing center / traffic diversion** | "Real" DDoS protection in the marketing sense; competes with Arbor/NetScout | Requires GRE tunnels, scrubbing infrastructure (hardware + bandwidth), reverse traffic path engineering. Months of work; out of scope per PROJECT.md. | Use RTBH + Flowspec; for advanced users, integrate with external scrubbers (Gcore, V.tal) via configurable webhook — but not in MVP. |
| **Deep Packet Inspection (DPI) detection** | "Catches more attacks" | Requires inline deployment or SPAN port at line rate; orders of magnitude more expensive than flow sampling. Doesn't add value for volumetric attacks (the MVP target). | Stick with sampled flows. They are sufficient for volumetric per the FastNetMon record-scale detection (1.5 Bpps caught in 2025 with sFlow). |
| **Application-layer (L7) attack detection** | HTTP flood / Slowloris are real attacks | Requires reverse-proxy / WAF integration; different architecture, different telemetry source. Already excluded in PROJECT.md. | Document clearly that Mitigador is L3/L4-only; recommend Cloudflare/BunnyShield/etc. for L7. |
| **Full PCAP capture during attacks** | "I need packet-level forensics" | Requires inline tap or mirror port at attack-rate; storage explodes (10Gbps attack = 75GB/min). Operational nightmare. | Provide flow-level "top talkers / vectors / sources" summary; that's 95% of what abuse reports need. Flowtriq's pre-attack ring buffer is the only sane PCAP pattern, and only viable for per-server detection. |
| **ML-based / "AI" detection in MVP** | Buzzword-driven; promises fewer false positives | Black-box ML on flow data is hard to tune, hard to explain ("why was X blackholed?"), and underperforms tuned thresholds with hostgroups. Adds operational complexity without proportional benefit. | Ship strong threshold + baseline-learning. Revisit ML in v2 only after operators describe specific patterns thresholds miss. |
| **Multi-organization SaaS portal** | "Sell it to other ISPs" | Adds auth complexity, billing, tenant isolation, compliance burden. PROJECT.md explicitly out-of-scope. | Each ISP runs their own install. Separate operational concern. |
| **Mobile native app** | "Operators are on phones" | Native iOS/Android maintenance burden, app store overhead. PROJECT.md out-of-scope. | Telegram bot is the mobile interface. Dashboard is responsive web. |
| **Inter-provider attack signaling (Sightline-style)** | Coordinate mitigation upstream | Standards (DOTS RFC 8811) exist but no BR ISP ecosystem to coordinate with. Premature. | Use upstream-provider BGP communities (already covered by RTBH); that *is* the signaling in practice. |
| **Network capacity planning / billing reports** | NetFlow tools (Kentik, ntopng) bundle these | Different product; bloats scope. Operators already have netflow analyzers. | Keep Mitigador focused on attack lifecycle. Export raw flow data via API if user wants to plug into ntopng. |
| **Custom rule scripting language** | "Let operators write their own detection rules" | Maintenance burden, security risk (sandbox escape), users won't actually write rules. | Provide rich pre-built vector detectors + per-hostgroup thresholds. If escape hatch needed: webhook on detection so user runs their own script. |
| **Ticketing system / SLA management** | "We want to track incidents end-to-end" | That's Jira/Zammad/GLPI's job. | Provide webhook integration so user POSTs to their own ticket system. |

---

## Feature Dependencies

```
[Flow ingestion (sFlow/NetFlow/IPFIX)]
    └──required-by──> [Per-host accounting]
                           └──required-by──> [Threshold detection]
                                                  ├──required-by──> [Attack record / history]
                                                  │                        └──required-by──> [Dashboard]
                                                  │                        └──required-by──> [Top talkers / reports]
                                                  ├──required-by──> [BGP RTBH announcement]
                                                  │                        └──required-by──> [Auto-unblock observer]
                                                  ├──required-by──> [BGP Flowspec announcement]
                                                  └──required-by──> [Alerting (Telegram / Email)]
                                                                           └──enhanced-by──> [Alert deduplication]
                                                                           └──enhanced-by──> [Inline approval (Telegram buttons)]

[Whitelist / trusted networks]
    └──gates──> [Any mitigation action]  (safety rail; checked before BGP announce)

[Baseline learning mode]
    └──feeds──> [Per-host threshold suggestion]
                     └──seeds──> [Threshold detection config]

[BGP session to router]
    └──required-by──> [RTBH announcement]
    └──required-by──> [Flowspec announcement]
    └──surface-state-to──> [Dashboard health view]
    └──surface-state-to──> [Alerting (if session down → page operator)]

[Authentication]
    └──required-by──> [Configuration UI]
    └──required-by──> [Audit log (records actor)]

[Multi-channel alerting] ──enhances──> [Operator confidence to enable auto-mitigation]
[Manual approval mode]   ──enhances──> [Operator confidence to enable auto-mitigation]
[Per-hostgroup thresholds] ──enhances──> [False positive rate]
[Per-hostgroup thresholds] ──conflicts──> [Naive global threshold UI]  (don't ship both)
```

### Dependency Notes

- **Flow ingestion → everything:** Detection, mitigation, alerting, reporting all sit downstream. This is the foundation phase.
- **Whitelist gates mitigation:** Implement the safety rail *before* enabling automated BGP announcement. A false positive that blackholes the NOC's own IPs is unrecoverable politically.
- **Baseline learning before auto-mitigate:** Operators should be able to run Mitigador in observe-only mode for ≥1 week before turning on actions. Forced by FastNetMon docs and consistent industry guidance.
- **Manual approval mode before full auto:** Per PROJECT.md constraint ("falsos positivos custam reputação"), shipping Telegram inline-approval before pure auto-mitigation is the trust-building path.
- **BGP session state is operationally critical:** If the BGP session is down, mitigation is silently broken. Surfacing this prominently is not optional.

---

## MVP Definition

### Launch With (v1) — "If the next attack happens tonight, this catches and mitigates it"

- [ ] **sFlow ingestion** — Mikrotik dominance in target market makes this non-negotiable
- [ ] **NetFlow v9 ingestion** — covers Cisco + most modern exporters; v5 can come later if a real user has only v5 gear
- [ ] **Per-host bps/pps accounting** — foundation for everything
- [ ] **Threshold-based detection** with **per-hostgroup thresholds** — the actual detector; hostgroups are needed even in v1 because otherwise it's unusable in mixed networks
- [ ] **UDP flood and ICMP flood vector classification** — the two attack types from the PROJECT.md trigger incident
- [ ] **BGP RTBH announcement** via gobgp or exabgp — the primary mitigation path
- [ ] **Configurable BGP communities per upstream** — RTBH is useless without correct community tags
- [ ] **Whitelist / trusted networks** — safety rail must ship with the first version that announces BGP
- [ ] **Manual approval mode (Telegram inline buttons)** — operator-in-the-loop before full automation; trust builder
- [ ] **Telegram alerts with attack details** — primary notification channel in BR ISP segment
- [ ] **Email alerts (SMTP)** — secondary channel, audit trail
- [ ] **Real-time dashboard** showing active attacks + recent history
- [ ] **Attack history persistence** with per-attack summary view (vector, peak, sources, action taken)
- [ ] **Authenticated web UI** — minimum: username/password, sessions, one role
- [ ] **Audit log** of all mitigation actions
- [ ] **BGP session health visible in dashboard**
- [ ] **Auto-withdrawal of RTBH after attack subsides** — operators forget; this prevents extended customer outages

### Add After Validation (v1.x) — "Operators have used it for a month, what do they ask for?"

- [ ] **IPFIX ingestion** — add when a user shows up with IPFIX-only gear (likely Juniper MX shop)
- [ ] **NetFlow v5 ingestion** — only if a real user has v5-only gear (rare in 2026)
- [ ] **BGP Flowspec announcement** — once RTBH is trusted and operators want surgical mitigation; non-trivial because Mikrotik Flowspec support is limited (test on customer's actual gear)
- [ ] **Baseline learning mode with auto-suggested thresholds** — solves the "what threshold do I set?" onboarding pain
- [ ] **Per-prefix threshold templates** (residential/corporate/gaming/DNS) — onboarding ergonomics
- [ ] **More attack vectors**: DNS/NTP/SSDP/Memcached amplification, SYN flood — extending classification once UDP/ICMP is solid
- [ ] **Top talkers / source-ASN report per attack** — for abuse reports to upstream peers
- [ ] **Alert deduplication tuning UI** — once operators report alert fatigue
- [ ] **Webhook integration on detection** — escape hatch for SIEM/ticketing/custom scripts
- [ ] **Configuration backup / restore API** — for multi-install ops
- [ ] **Grafana datasource / dashboard export** — many ISPs already have Grafana, want to bolt Mitigador metrics in

### Future Consideration (v2+) — "Only after PMF; some may never ship"

- [ ] **External scrubbing integration** (Gcore / V.tal / Voxility webhook) — only if BR market shows demand
- [ ] **DOTS / inter-provider signaling (RFC 8811)** — premature; revisit when peer ISPs adopt
- [ ] **L7 attack detection** — explicitly out of scope per PROJECT.md; revisit only with strong customer pull
- [ ] **ML-based anomaly detection** — only if specific attack patterns elude tuned thresholds in production
- [ ] **Multi-org SaaS** — explicitly out of scope per PROJECT.md
- [ ] **Mobile native app** — explicitly out of scope; Telegram is the mobile interface
- [ ] **PCAP capture** — explicitly anti-feature for ISP-edge use case
- [ ] **RBAC with multiple roles** — single-tenant install model means "admin + read-only" is likely sufficient; only expand if operators ask

---

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| sFlow ingestion | HIGH | MEDIUM | P1 |
| NetFlow v9 ingestion | HIGH | MEDIUM | P1 |
| Per-host accounting | HIGH | MEDIUM | P1 |
| Threshold detection (hostgroups) | HIGH | MEDIUM | P1 |
| UDP/ICMP flood classification | HIGH | LOW | P1 |
| BGP RTBH announcement | HIGH | MEDIUM | P1 |
| Per-upstream BGP communities | HIGH | LOW | P1 |
| Whitelist / trusted networks | HIGH | LOW | P1 |
| Manual approval (Telegram inline) | HIGH | MEDIUM | P1 |
| Telegram alerts | HIGH | LOW | P1 |
| Email alerts | MEDIUM | LOW | P1 |
| Real-time dashboard | HIGH | MEDIUM | P1 |
| Attack history persistence | HIGH | MEDIUM | P1 |
| Authenticated UI | HIGH | LOW | P1 |
| Audit log | MEDIUM | LOW | P1 |
| Auto-unblock after attack | HIGH | MEDIUM | P1 |
| BGP session health view | MEDIUM | LOW | P1 |
| IPFIX ingestion | MEDIUM | MEDIUM | P2 |
| BGP Flowspec | MEDIUM | HIGH | P2 |
| Baseline learning mode | HIGH | MEDIUM | P2 |
| Per-prefix templates | MEDIUM | LOW | P2 |
| Top talkers report | MEDIUM | MEDIUM | P2 |
| More attack vectors (DNS/NTP amp) | MEDIUM | LOW | P2 |
| Webhook on detection | MEDIUM | LOW | P2 |
| NetFlow v5 ingestion | LOW | LOW | P2 |
| Grafana export | LOW | LOW | P2 |
| Alert deduplication tuning UI | MEDIUM | MEDIUM | P2 |
| External scrubbing integration | LOW | HIGH | P3 |
| L7 detection | LOW (out-of-scope) | HIGH | P3 (anti-feature) |
| ML detection | LOW | HIGH | P3 |
| PCAP capture | LOW | HIGH | P3 (anti-feature) |
| Multi-org SaaS | LOW (out-of-scope) | HIGH | P3 (anti-feature) |
| Mobile app | LOW (out-of-scope) | HIGH | P3 (anti-feature) |

**Priority key:**
- **P1:** Must have for launch. If any P1 is missing, the next attack catches the operator unprepared.
- **P2:** Should have, add when possible. Real users will request these within first month of production.
- **P3:** Defer indefinitely or never build. Many are explicit anti-features.

---

## Competitor Feature Analysis

| Feature | FastNetMon Advanced | Wanguard (Andrisoft) | Arbor Sightline | lupael/ddos-protection (ref) | **Our Approach (Mitigador)** |
|---------|---------------------|----------------------|-----------------|-------------------------------|------------------------------|
| sFlow/NetFlow/IPFIX | Yes (all) | Yes (all) + DPDK packet capture | Yes (all) + proprietary ASI | Yes (per project description) | Yes — sFlow + NetFlow v9 at v1, IPFIX in v1.x |
| BGP RTBH | Yes (via ExaBGP/GoBGP/BIRD) | Yes (native) | Yes (native) | Yes | Yes — gobgp embedded |
| BGP Flowspec | Yes | Yes | Yes | Yes | v1.x — depends on Mikrotik FS support testing |
| Telegram alerts | Yes (Advanced only) | No (email/SNMP/script) | No | Yes (per project description) | Yes — **first-class, with inline approval buttons** |
| Email alerts | Yes (Advanced) | Yes | Yes | Yes | Yes |
| Slack/PagerDuty | Yes (Advanced) | Via webhook script | Yes | Likely no | Webhook in v1.x; native PagerDuty deferred |
| Per-hostgroup thresholds | Yes (Advanced) | Yes (subnet groups) | Yes (managed objects) | Unknown | Yes — P1 |
| Baseline learning | Yes (Advanced) | Yes | Yes (ML-driven) | No | v1.x |
| Manual approval mode | Limited | Limited | Yes (operator workflow) | No | **Yes — Telegram inline-button approval is a differentiator** |
| Multi-tenant SaaS | No (single install) | No | Yes (managed service offering) | No | No — separate-install model per PROJECT.md |
| RBAC | Basic | Yes | Yes (deep) | No | Single admin role at v1; expand only if asked |
| PCAP capture | No | No | Limited | No | **No (deliberately anti-feature)** |
| Scrubbing integration | Yes (Gcore native, 2025) | Yes | Yes (Arbor TMS) | No | v2+ via webhook; not built-in |
| Portuguese UI / BR defaults | No | No | No | Possibly | **Yes — primary differentiator in BR market** |
| Mikrotik first-class support | Generic | Generic | Generic | Mikrotik-aware (per project description) | **Yes — primary differentiator** |
| Price | $115–$350/mo per instance | Commercial (quote-based) | Enterprise ($$$) | Free | Free (self-host) |

**Strategic positioning:** Mitigador occupies the gap between (a) FastNetMon Community (free but Telegram-less, threshold-rough, no UI for non-technical operators) and (b) FastNetMon Advanced / Wanguard (priced for medium-large ISPs, English-first, generic vendor support). The wedge is "Brazilian small/medium ISP with Mikrotik, runs on Telegram, operator approves blackholes from their phone."

---

## Brazilian ISP Context Notes

- **Telegram is the operational chat / alerting standard** in BR ISP segment. Every MUM (MikroTik User Meeting) Brazil presentation and provider blog uses Telegram for The Dude / monitoring alerts. Email is for record; Telegram is for "fix it now."
- **Mikrotik dominates small/medium ISP edge.** Juniper appears at larger ISPs and aggregation; Cisco at legacy / specific niches. Mikrotik's BGP Flowspec support is recent (RouterOS 7.x) and **must be validated on the operator's actual hardware** before claiming support.
- **Upstream community conventions vary** — major BR transit/peering (Telxius/Lumen/Algar/V.tal/IX.br) have their own blackhole BGP communities. Pre-loading these as templates is high-leverage onboarding work.
- **Portuguese-first UI** matters: most BR NOC operators read English technical docs but configure tools in Portuguese. UI labels, alert text, and docs in pt-BR is a real differentiator.
- **Cost sensitivity is severe.** Most small BR ISPs cannot justify FastNetMon Advanced ($115+/mo per instance) or Wanguard. The market gap is real.
- **Operators are bandwidth-constrained for telemetry.** Sample rates of 1:1000 or 1:2000 on sFlow are common; the detector must work well at these rates.

---

## Sources

- [FastNetMon Product Overview](https://fastnetmon.com/product-overview/) (HIGH — official)
- [FastNetMon Community vs Advanced comparison](https://fastnetmon.com/compare-community-and-advanced/) (HIGH — official)
- [FastNetMon hostgroup / per-host threshold docs](https://fastnetmon.com/docs-fnm-advanced/fastnetmon-advanced-per-host-threshold-configuration/) (HIGH — official)
- [FastNetMon automated baseline calculation](https://fastnetmon.com/docs-fnm-advanced/automated-baseline-calculation-with-fastnetmon-advanced/) (HIGH — official)
- [FastNetMon Filtering L3/L4 with Flowspec + RTBH practical guide](https://fastnetmon.com/2025/07/08/filtering-l3-l4-ddos-attacks-with-bgp-flow-spec-and-rtbh-a-practical-guide-for-engineers/) (HIGH — official, 2025)
- [Wanguard product page](https://www.andrisoft.com/software/wanguard) (HIGH — official)
- [Wanguard BGP Connector docs](https://docs.andrisoft.com/wanguard/8.4/Configuration__Components__BGP_Connector.html) (HIGH — official)
- [Wanguard mitigation method choice](https://docs.andrisoft.com/wanguard/8.5/Choosing_a_Method_of_DDoS_Mitigation.html) (HIGH — official)
- [Arbor Sightline product page (NETSCOUT)](https://www.netscout.com/product/arbor-sightline) (HIGH — official)
- [Arbor Sightline with Sentinel](https://www.netscout.com/product/arbor-sightline-sentinel) (HIGH — official)
- [Flowtriq: FastNetMon vs Wanguard vs Flowtriq comparison (2026)](https://flowtriq.com/blog/fastnetmon-vs-wanguard-vs-flowtriq) (MEDIUM — vendor blog, but detailed)
- [Flowtriq: Best DDoS Detection Tools 2026](https://flowtriq.com/blog/best-ddos-detection-tools) (MEDIUM — vendor blog)
- [Flowtriq: PagerDuty escalation for DDoS](https://flowtriq.com/blog/pagerduty-escalation) (MEDIUM — vendor blog)
- [sflow-rt/ddos-protect (BGP RTBH + FlowSpec reference impl)](https://github.com/sflow-rt/ddos-protect) (HIGH — open source reference)
- [Kentik: Network anomaly detection guide](https://www.kentik.com/kentipedia/network-anomaly-detection/) (MEDIUM)
- [Kentik: DDoS protection 2026 guide](https://www.kentik.com/kentipedia/ddos-protection/) (MEDIUM)
- [Nokia: Baselines and thresholds for DDoS](https://www.nokia.com/blog/do-baselines-and-thresholds-work-to-protect-critical-unpredictable-ip-networks-from-ddos-attacks/) (MEDIUM — vendor)
- [Cloudflare: UDP Flood DDoS](https://www.cloudflare.com/learning/ddos/udp-flood-ddos-attack/) (HIGH — vendor docs)
- [Imperva: Ping/ICMP Flood DDoS](https://www.imperva.com/learn/ddos/ping-icmp-flood/) (HIGH — vendor docs)
- [NETSCOUT: Reflection Amplification](https://www.netscout.com/what-is-ddos/what-is-reflection-amplification-attack) (HIGH — vendor docs)
- [Made4It: Blackhole BGP Mikrotik (BR)](https://made4it.com.br/en/blackhole-bgp-mikrotik/) (MEDIUM — BR practitioner)
- [Remontti blog: FastNetMon + Mikrotik blackhole (BR)](https://blog.remontti.com.br/3981) (MEDIUM — BR practitioner, in pt-BR)
- [MikroTik MUM presentation: DDoS detection and mitigation](https://mum.mikrotik.com/presentations/EU16/presentation_2960_1456752556.pdf) (MEDIUM — community)
- [MikroTik MUM Brasil: DDoS attacks](https://mum.mikrotik.com/presentations/BR16/presentation_3694_1480438361.pdf) (MEDIUM — community, BR-specific)
- [Telic: BR DDoS mitigation provider](https://telic.com.br/mitigacao_ddos/) (LOW — competitor marketing)
- [PagerDuty alert deduplication (Events API v2)](https://support.pagerduty.com/main/docs/alerts) (HIGH — official)
- [F5: DDoS threshold modes incl. Learn Only](https://community.f5.com/kb/technicalarticles/explanation-of-f5-ddos-threshold-modes/286884) (HIGH)
- [Radware: Smart RBAC for multi-tenant DDoS](https://www.radware.com/blog/ddos-protection/2023/08/streamlining-ddos-and-adc-environment-management-and-operation-with-smart-rbac/) (MEDIUM — vendor)

**Note on lupael/ddos-protection:** The GitHub repo at `github.com/lupael/ddos-protection` returned 404 at research time (2026-05-17). Features inferred from the PROJECT.md description (sFlow/NetFlow/IPFIX, RTBH/Flowspec, Telegram/email alerts, multi-tenant, dashboard) rather than directly read. Recommend the team verify the repository exists and confirm feature set before fork/base decision.

---

*Feature research for: DDoS mitigation platform for ISPs (Brazilian small/medium market, volumetric MVP focus)*
*Researched: 2026-05-17*
