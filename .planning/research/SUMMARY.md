# Project Research Summary

**Project:** Mitigador
**Domain:** DDoS detection & mitigation platform for small/medium Brazilian ISPs (volumetric attacks, BGP-based response)
**Researched:** 2026-05-17
**Confidence:** HIGH

## Executive Summary

Mitigador é uma plataforma de detecção e mitigação automática de DDoS volumétrico para ISPs brasileiros pequenos/médios. Ingere telemetria de roteadores de borda (NetFlow/IPFIX/sFlow), detecta floods UDP/ICMP e padrões de carpet-bombing em segundos, e anuncia rotas BGP (RTBH + Flowspec) para descartar tráfego de ataque no edge — tudo com alertas em tempo real via Telegram, e-mail e dashboard web. A pesquisa confirma que é uma categoria madura com building blocks OSS sólidos (FastNetMon Community, GoBGP, GoFlow2), portanto o risco de engenharia está em integração e segurança operacional, não em novidade algorítmica.

**Achados críticos que precisam ser endereçados antes de criar o roadmap:**

1. **Correção sobre `lupael/ddos-protection`:** pesquisa inicial bateu 404 porque o nome correto do repo tem typo: `lupael/ddos-potection` (sem o "r"). O repo **existe** (MIT, atualizado em 2026-04-23, 2 stars). Stack: **Python 3.11 + FastAPI + React 18 + Docker Compose + PostgreSQL + Redis + Prometheus + Grafana**. Features listadas: NetFlow/sFlow/IPFIX, RTBH via ExaBGP/FRR/BIRD, Flowspec, MikroTik API direta, multi-tenant com RBAC, Telegram, alertas e dashboard. Tem `docker-compose.yml`, `kubernetes/`, `project-docs/`. Repo é recente e ainda pouco testado em produção (2 stars, 1 contribuidor).

   **Comparação com a recomendação da pesquisa (FastNetMon + Go):**
   - lupael é **mais completo em escopo** (já tem dashboard, multi-tenant, Telegram, integração Mikrotik) mas é **menos testado** e **escolhe stack diferente** (Python/FastAPI vs Go; Docker vs systemd binary; React vs Vue).
   - FastNetMon Community é **engine de detecção maduro** (apontado pelo próprio usuário como tal), mas é só detecção — toda orquestração (BGP, dashboard, Telegram, multi-tenant) precisa ser construída.
   - **Decisão pendente:** fork lupael e estender, OU usar FastNetMon como engine e construir orquestração (recomendação da pesquisa stack).

2. **Mikrotik (roteador dominante no mercado-alvo) NÃO suporta sFlow nem BGP Flowspec.** RouterOS exporta apenas NetFlow v1/v5/v9 e IPFIX; Flowspec é feature request aberta em v7. PROJECT.md trata sFlow e Flowspec como universais — não são. O MVP precisa ingerir **NetFlow v9 + IPFIX** como primário (sFlow apenas para Juniper/Cisco), e mitigação fica em **RTBH para peers Mikrotik** com Flowspec só nos peers Juniper/Cisco. Além disso, Mikrotik tem dois bugs específicos: (a) RouterOS v6.49.6+ codifica sampling rate do NetFlow v9 com byte-order errado (causa ~1000x erro de detecção), e (b) RouterOS v7 não remove sempre a flag de blackhole da FIB quando a community é retirada (blackhole "preso").

Riscos operacionais críticos: **falsos positivos** (blackholar cliente real é pior que perder um ataque — PROJECT.md diz isso explicitamente), **carpet-bombing** (thresholds por IP perdem ataques distribuídos em /22, padrão dominante 2024-2026), e **route leak BGP** (Cloudflare 1.1.1.1 caiu em 2024 por exatamente isso). Mitigações: dry-run como default inicial; thresholds por prefixo com agregação multi-resolução (/32, /28, /24, /22); sessão BGP de mitigação dedicada com filtros de saída estritos e origin check; panic button operacional desde o dia 1.

## Key Findings

### Stack Recomendado

Stack convergiu em **binário Go estático + systemd** alinhado com normas operacionais de ISP (sem Docker no box de data plane, instalação `.deb`/`.rpm` via goreleaser). Detecção delegada ao FastNetMon Community 1.2.8 (Apache 2.0, único OSS maduro que de fato detecta — não só coleta); orquestração em Go porque o ecossistema BGP/flow é Go-native (GoBGP, GoFlow2). Frontend Vue 3 + Vite + Naive UI + Apache ECharts com SSE para updates em tempo real.

**Tecnologias core:**
- **FastNetMon Community 1.2.8** — engine de detecção DDoS (Apache 2.0)
- **GoBGP v4.5+** — speaker BGP (RTBH + Flowspec), embeddável como lib Go
- **Go 1.22+** — orquestração backend (alinhamento ecossistema)
- **chi v5** — router HTTP (stdlib-friendly, SSE-friendly)
- **PostgreSQL 16** — incidentes, config, audit log
- **ClickHouse 24.x** — arquivo de flows (Phase 4+, opcional)
- **Vue 3 + Vite + Naive UI** — dashboard SPA
- **Apache ECharts** — visualização
- **go-telegram/bot** — Telegram (NÃO usar `go-telegram-bot-api/telegram-bot-api` — abandonado 2021)
- **wneessen/go-mail** — SMTP (NÃO usar `go-gomail/gomail` — manutenção)
- **systemd + goreleaser** — deployment

Detalhe completo: `.planning/research/STACK.md`.

### Features Esperadas

Wedge competitivo está em **ISP brasileiro + Mikrotik + Telegram-first**: UI em pt-BR, communities BGP pré-carregadas (Telxius/Lumen/Algar/V.tal/IX.br), templates Mikrotik first-class, Telegram-as-CLI com inline-approval buttons. Resto é paridade com FastNetMon Advanced / Wanguard / Arbor.

**Must-have (P1):**
- Ingestão NetFlow v9 + IPFIX (NÃO sFlow primário — Mikrotik não exporta)
- Per-host pps/bps com janelas deslizantes
- Thresholds **por prefixo/hostgroup** (globais são inutilizáveis)
- Classificação UDP/ICMP flood
- BGP RTBH via GoBGP embeddado, communities por upstream configuráveis
- Whitelist / trusted-networks (deve estar com primeiro release que anuncia BGP)
- Modo manual-approve via Telegram inline buttons (operator-in-loop antes de full auto)
- Alertas Telegram + SMTP + dashboard SSE
- Dashboard real-time autenticado
- Audit log imutável append-only
- Saúde de sessão BGP visível
- Auto-withdraw RTBH após ataque (com TTL safety net)
- Panic button (web + Telegram + CLI standalone)

**Differentiators (P1.x):**
- Defaults brasileiros (communities pré-carregadas, UI pt-BR)
- Templates Mikrotik first-class
- Modo baseline learning (1-2 semanas → sugestão de thresholds)
- Telegram bot como CLI (`/status`, `/active`, `/unblock`, `/panic_stop_all`)
- Templates de threshold por perfil (residencial/corporate/gaming/DNS)
- BGP Flowspec — **apenas para peers Juniper/Cisco**
- Top-talkers / source-ASN por ataque
- Carpet-bombing detection (multi-resolução /32 a /22)

**Defer (v2+ ou nunca):**
- sFlow ingestion (apenas quando aparecer usuário Juniper/Cisco)
- ML-based detection (thresholds bem tunados batem; revisitar pós-PMF)
- Scrubbing center (out of scope)
- L7 detection (out of scope)
- PCAP completo (storage explode)
- Multi-org SaaS (PROJECT.md decidiu contra)
- Mobile native app (Telegram é a interface móvel)

Detalhe: `.planning/research/FEATURES.md`.

### Abordagem Arquitetural

**Pipeline single-binary com canais in-process** é a forma certa de MVP: Collector → Aggregator → Detector → Mitigator/Alerter/Storage roda como um processo Go com goroutines e canais buffered. Evita o peso operacional de Kafka/Redis para ISPs pequenos (1-10 Gbps) enquanto mantém latência baixa e debugging simples. GoBGP embeddado como library (in-process), não sidecar. Multi-tenant via **systemd instantiated units** (`mitigador@isp.service`, `mitigador@cliente.service`) — cada um com config, schema DB e sessão BGP próprios — não multi-tenancy de shared-binary.

**Split hot path / cold path é inegociável:** apenas últimos 60s de counters por host em RAM; apenas sumários de incidente persistidos; flow records crus são consumidos e descartados (persisti-los enche disco em dias).

**Componentes:**
1. **Flow Collector** — listeners UDP 2055/4739/6343; template cache NetFlow/IPFIX; validação de IP de origem
2. **Aggregator** — counters per-host/per-prefix em sliding-window (1-5s buckets, últimos 60s em RAM); contagem em múltiplos prefix lengths (/32, /28, /24, /22 — para carpet-bombing)
3. **Detection Engine** — tick 1s, thresholds per-tenant/per-prefix, multi-criteria (pps AND bps AND duração), scoring de confiança
4. **Mitigation Engine** — decide RTBH vs Flowspec por vendor; modos dry-run/manual-approve/auto; origin-check; TTL obrigatório
5. **BGP Speaker (GoBGP embeddado)** — eBGP com peer dedicado de mitigação, TCP-MD5, filtro saída estrito
6. **Alert Dispatcher** — fan-out paralelo Telegram + SMTP + SSE; agregação + dedup + rate-limit
7. **Storage** — Postgres (incidentes/config/audit); ClickHouse opcional (flow archive Phase 4+); retenção tiered
8. **API + Dashboard** — chi + Vue 3 SPA; SSE para live feed; **panic button** com teste trimestral

**Budget de latência:** sample do roteador → mitigação anunciada ≤ 7s.

Detalhe: `.planning/research/ARCHITECTURE.md`.

### Pitfalls Críticos

1. **Blackholar /32 errado (false-positive customer outage)** — reputação destruída para sempre. Mitigação: dry-run default; thresholds por prefixo; multi-criteria; allow-list VIP exigindo manual approve; scoring de confiança.

2. **Evasão por carpet-bombing** — atacante espalha 12 Mbps × 1000 IPs num /22 = 12 Gbps total saturando link, mas nenhum threshold por IP dispara. **Padrão dominante 2024-2026.** Mitigação: agregação simultânea em /32, /28, /24, /22; watchdog de utilização total do uplink; Flowspec para mitigação por padrão (não /32-por-/32 RTBH).

3. **Route leak BGP (RTBH /32 escapa para upstream)** — Cloudflare 1.1.1.1 ficou fora em 2024 por isso. Mitigação: sessão BGP dedicada; filtro saída estrito strippando community blackhole nas outras sessões; origin check; RPKI validation; audit log imutável.

4. **Cegueira de sampling-rate + bug Mikrotik byte-order** — Mikrotik codifica sampling rate do NetFlow v9 invertido, causando erro de ~1000x. Mitigação: hard-code do sampling rate no coletor (`netflow_ignore_sampling_rate_from_device`); cross-check PPS contra contadores do link.

5. **Stuck blackhole (Mikrotik não remove flag)** — bug RouterOS v7: flag blackhole fica na FIB mesmo após withdraw BGP. Cliente fica offline horas após ataque acabar. Mitigação: TTL obrigatório em toda announcement (auto-withdraw 30min); verificação pós-withdraw (ping no alvo); route-refresh BGP periódico nos peers Mikrotik; reconciliação de estado no startup.

6. **Sem escape hatch** — operador não consegue parar engine descontrolado. Mitigação: **panic button** big-red no UI (mobile-friendly); `/panic_stop_all` no Telegram; CLI standalone `mitigador panic --withdraw-all` que funciona sem DB/web.

Outros pitfalls cobertos no PITFALLS.md: detection lag (NetFlow timeout 60s default derrota tudo — setar 1-10s); ferramenta como attack surface (auth em toda interface, TCP-MD5 BGP); storage explosion (retenção tiered desde dia 1); LGPD/Marco Civil (retenção enforced, cláusula contratual, audit log); alert storm (limites Telegram 30 msg/s).

Detalhe: `.planning/research/PITFALLS.md`.

## Implications for Roadmap

Pesquisa sugere **7 fases (Phase 0 + 6 fases de feature)** progressivas construindo a espinha (telemetria → detecção → mitigação) antes dos membros (Flowspec, UX avançado, multi-tenant). Cada fase entrega algo operacionalmente útil e endereça os pitfalls mais arriscados cedo.

### Phase 0: Foundation & Re-Scoping
- Fechar ADRs: (a) "no fork — build greenfield on FastNetMon", (b) "NetFlow v9 + IPFIX primário, sFlow deferred", (c) "RTBH primário, Flowspec Juniper/Cisco only"
- Documentar inventário do ISP do amigo (vendor, modelo, versão, BGP, upstreams)
- Lab FastNetMon + GoBGP em VM, testar end-to-end com export Mikrotik real
- **Sem código** — só decisões e validação

### Phase 1: Telemetry Spine + Read-Only Observation (MVP slice 1)
- FastNetMon Community + Go wrapper + NetFlow v9/IPFIX
- Per-host accounting + dashboard read-only Vue+SSE
- Alertas Telegram (sem mitigação ainda)
- Override de sampling rate (workaround Mikrotik byte-order)
- **Sem BGP** — observação pura primeiro
- Research flag: contrato do notify-script do FastNetMon

### Phase 2: BGP Mitigation (RTBH-only) + Safety Rails
- GoBGP embeddado + sessão BGP dedicada + TCP-MD5 + filtro saída estrito
- RTBH announce com communities por upstream + whitelist + TTL
- **Manual-approve via Telegram inline buttons** como default
- **Dry-run mode** + **panic button** (web + Telegram + CLI standalone)
- Mikrotik route-refresh workaround + audit log imutável
- Research flag: bug Mikrotik atual + matriz communities BR + RPKI ROA

### Phase 3: Multi-Tenant Config + Operator UX
- Auth (single admin role via scs/v2)
- CRUD UI: hostgroups, thresholds, peers, alert channels, whitelist
- Templates BR (communities pré-carregadas) + UI pt-BR
- systemd instantiated units (`mitigador@isp` vs `mitigador@cliente`)
- Validação de config no startup
- Sem research flag (padrões web app standard)

### Phase 4: Carpet-Bombing Detection + Reporting
- Agregação multi-resolução /32 + /28 + /24 + /22
- Watchdog total-uplink-utilization
- Attack timeline view + top source ASNs + baseline learning (1-2 semanas)
- Alert aggregation + severity throttling + dedup
- ClickHouse entra aqui (flow archive para top-talker queries)
- Research flag: Count-Min/HLL sketches; interface FastNetMon para per-prefix

### Phase 5: Flowspec Mitigation (Juniper/Cisco) + IPv6
- Flowspec NLRI validado por RFC 9117 (não só 5575)
- Capability probe no startup + fallback RTBH se peer rejeita
- Dual-stack (IPv4 + IPv6) em detecção + mitigação + RTBH + Flowspec
- Research flag: matriz aceitação Flowspec upstreams BR

### Phase 6: LGPD/Marco Civil Compliance + Hardening
- Retenção tiered enforced (raw ≤72h, per-minute 30d, per-hour 1y)
- Legal-basis docs + opt-out config para SLA-clients + DPO contact
- Matriz hardware Mikrotik suportado + secrets management (vault/sops)
- Rate limit hard cap; TCP-MD5 enforced; dependency scanning; signed releases; SBOM
- Research flag: precedentes LGPD/ANATEL para tráfego ISP

### Phase Ordering Rationale

- **Dependências ditam a espinha:** flow ingestion bloqueia tudo (Phase 1); detection bloqueia alerting/mitigation; mitigation bloqueia utilidade produção mas é maior risco → fase própria (Phase 2)
- **Risk-front-loading:** as duas categorias mais perigosas — false-positive blackhole (Phase 2) e missing carpet-bombing (Phase 4) — ganham fases dedicadas, não comprimidas em "feature work"
- **Realidade de vendor:** Phase 1 ingere NetFlow/IPFIX (Mikrotik-friendly), Phase 2 mitiga via RTBH (universal), Phase 5 adiciona Flowspec (Juniper/Cisco-only). MVP (Phases 1-3) funciona em ISP 100% Mikrotik — exatamente o cenário do amigo.
- **Confiança operacional antes de automação:** Phase 1 observação-pura, Phase 2 dry-run + manual-approve defaults, Phase 3 config legível. Auto-mitigation só ligado após Phase 4 (baseline + multi-resolução validados).

### Research Flags (precisam `/gsd-research-phase`)

- Phase 1, 2, 4, 5, 6

### Sem Research Flag (padrões standard)

- Phase 0 (decisões + inventário), Phase 3 (CRUD web app)

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Recommendations verified contra docs oficiais + releases recentes; alternativas explicitamente consideradas; 404 do `lupael/ddos-protection` verificado direto; limitações Mikrotik verificadas em docs oficiais + feature requests do fórum |
| Features | HIGH | Paridade comercial verificada nas docs de FastNetMon Advanced/Wanguard/Arbor; MEDIUM em normas BR (inferido de MUM-BR, blogs pt-BR, sem dados formais de mercado) |
| Architecture | HIGH | Componentes + data flow consistentes entre FastNetMon AMS-IX deep-dive, goflow2 architecture, ExaBGP integration; MEDIUM em throughput numbers (vendor benchmarks têm viés) |
| Pitfalls | HIGH em pitfalls técnicos/operacionais (FastNetMon é incomumente franco; Cloudflare publicou incidentes; bugs Mikrotik filed em GitHub issues + fórum); MEDIUM em legal BR (Marco Civil + LGPD para tráfego ISP ainda evoluindo — sem precedente ANPD específico) |

**Overall confidence:** HIGH em recomendações técnicas; MEDIUM em normas BR-específicas (legal + community conventions) que precisam validação on-the-ground com o ISP do amigo.

### Gaps to Address

- **Inventário real do ISP do amigo (mix de vendors, topologia BGP, upstream peers)** — resolver em Phase 0
- **Matriz communities BGP upstreams BR (Telxius/Lumen/Algar/V.tal/IX.br)** — resolver em Phase 2
- **Aceitação Flowspec upstreams BR por peer** — resolver empiricamente em Phase 5
- **Status atual dos bugs Mikrotik (RTBH withdrawal + NetFlow byte-order) em RouterOS v7.x latest** — verificar em lab Phase 1 e 2
- **Precedente LGPD ANPD para tráfego DDoS de ISP** — consultar counsel em Phase 6
- **Teto de features do FastNetMon Community vs Advanced** — resolver incrementalmente Phases 4-5; fallback documentado para Go puro com GoFlow2

## Sources

### Primary (HIGH confidence)

- [FastNetMon GitHub + docs](https://github.com/pavel-odintsov/fastnetmon) (v1.2.8, AMS-IX deep-dive, threshold guidance, Mikrotik integration)
- [GoBGP repo + Flowspec docs](https://github.com/osrg/gobgp) (v4.5+, RFC 5575 + v6 Flowspec)
- [GoFlow2](https://github.com/netsampler/goflow2) (v2.2.6)
- [MikroTik Traffic Flow docs](https://help.mikrotik.com/docs/spaces/ROS/pages/21102653/Traffic+Flow) (confirma NetFlow v1/v5/v9 + IPFIX, sem sFlow)
- [MikroTik sFlow FR](https://forum.mikrotik.com/t/add-sflow/142506) + [Flowspec FR](https://forum.mikrotik.com/t/request-feature-bgp-dynamic-neighbors-bgp-flowspec/149201)
- [FastNetMon issue #985 — Mikrotik NetFlow v9 byte-order](https://github.com/pavel-odintsov/fastnetmon/issues/985)
- [MikroTik forum — RTBH flag stuck](https://forum.mikrotik.com/t/bgp-rtbh-blackhole-flag-not-removed-when-community-is-withdrawn/181566)
- [Cloudflare 1.1.1.1 incident, June 27 2024](https://blog.cloudflare.com/cloudflare-1111-incident-on-june-27-2024/)
- [RFC 7999 BLACKHOLE](https://www.rfc-editor.org/rfc/rfc7999.html), [RFC 5635 RTBH+uRPF](https://datatracker.ietf.org/doc/html/rfc5635), [RFC 9117 Flowspec Validation](https://datatracker.ietf.org/doc/rfc9117/)
- [Cisco IOS XR BGP Flowspec](https://www.cisco.com/c/en/us/td/docs/iosxr/cisco8000/bgp/b-bgp-config-cisco8000/m-bgp-flowspec.html), [Juniper Day One BGP Flowspec](https://www.juniper.net/documentation/en_US/day-one-books/DO_BGP_FLowspec.pdf)
- [Wanguard BGP Connector docs](https://docs.andrisoft.com/wanguard/8.4/Configuration__Components__BGP_Connector.html), [Arbor Sightline](https://www.netscout.com/product/arbor-sightline)
- [NETSCOUT carpet-bombing](https://www.netscout.com/blog/asert/carpet-bombing), [A10 carpet-bombing](https://www.a10networks.com/blog/carpet-bombing-ddos-the-attack-pattern-your-per-ip-defenses-wont-catch/)

### Reference repo (verified existence)

- `https://github.com/lupael/ddos-potection` — **existe**, MIT, Python 3.11 + FastAPI + React 18 + Docker. Pesquisa inicial bateu 404 porque agentes corrigiram o typo "potection" → "protection". Repo descrito como "ISP-level DDoS mitigation and network scrubbing framework". Topics incluem `bgp-mitigation`, `mikrotik-security`, `ddos-detection`. Última atualização 2026-04-23, 2 stars, 1 contribuidor. Stack incompatível com recomendação da pesquisa (Python/FastAPI/Docker vs Go/systemd-binary).

### Secondary (MEDIUM confidence)

- [Akvorado](https://github.com/akvorado/akvorado) (ClickHouse-based collector, AGPLv3)
- [Flowtriq blog](https://flowtriq.com/blog/fastnetmon-vs-wanguard-vs-flowtriq), [MUM Brasil presentations](https://mum.mikrotik.com/presentations/BR16/presentation_3694_1480438361.pdf)
- [Made4It](https://made4it.com.br/en/blackhole-bgp-mikrotik/), [Remontti pt-BR](https://blog.remontti.com.br/3981)
- [Cloudflare SP DDoS reference architecture](https://developers.cloudflare.com/reference-architecture/diagrams/network/protecting-sp-networks-from-ddos/)
- [ClickHouse vs TimescaleDB vs InfluxDB 2026](https://sanj.dev/post/clickhouse-timescaledb-influxdb-time-series-comparison)
- [PagerDuty alert dedup](https://support.pagerduty.com/main/docs/alerts)

---
*Research completed: 2026-05-17*
*Ready for roadmap: yes*
