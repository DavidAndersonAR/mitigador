# Mitigador

## What This Is

Plataforma de detecção e mitigação automática de ataques DDoS volumétricos voltada para ISPs (provedores de internet) e clientes corporativos. Coleta telemetria de tráfego dos roteadores de borda (sFlow / NetFlow / IPFIX), identifica ataques que tipicamente passam despercebidos pelo monitoramento tradicional e responde automaticamente anunciando rotas de mitigação via BGP (RTBH e Flowspec) — tudo com alertas em tempo real por Telegram, e-mail e dashboard web.

## Core Value

**Tornar visível e mitigar automaticamente o ataque volumétrico que hoje só é descoberto quando o link já caiu.**
Se tudo mais falhar, o operador precisa receber o alerta e ver a rota de blackhole/flowspec sendo anunciada antes do cliente reclamar.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

(Ainda nada — projeto greenfield, validar com a operação real do ISP do amigo)

### Active

<!-- Hipóteses até estarem em produção. -->

- [ ] Ingestão de telemetria via **NetFlow v9** (Mikrotik primary, universal)
- [ ] Ingestão de telemetria via **IPFIX** (equipamentos modernos)
- [ ] Ingestão de telemetria via **sFlow** (Juniper/Cisco onde disponível — Mikrotik não suporta)
- [ ] Detecção de ataques **volumétricos UDP flood** (per-host)
- [ ] Detecção de ataques **volumétricos ICMP flood** (per-host)
- [ ] **Detecção de carpet-bombing** via agregação multi-resolução (/32, /28, /24, /22) — padrão dominante 2024-2026
- [ ] Mitigação automática via **BGP RTBH** (blackhole de /32 atacado — universal, inclusive Mikrotik)
- [ ] Mitigação automática via **BGP Flowspec** (regras granulares — apenas peers Juniper/Cisco; Mikrotik não suporta)
- [ ] **Detecção de vendor por peer** + escolha automática RTBH vs Flowspec
- [ ] **Alertas Telegram** com detalhes do ataque (IP alvo, vetor, taxa)
- [ ] **Alertas por e-mail** com sumário de incidente
- [ ] **Dashboard web em tempo real** com ataques ativos e histórico
- [ ] **Modo multi-tenant**: instalação separada para ISP e cliente corporativo, com configurações independentes
- [ ] Suporte a roteadores **Mikrotik, Juniper e Cisco** (sessão BGP de mitigação)
- [ ] Configuração de **thresholds por prefixo/cliente** (não thresholds estáticos globais)
- [ ] **Histórico de ataques** persistido para análise post-mortem
- [ ] **Workaround Mikrotik NetFlow v9 byte-order** (sampling rate hard-coded no coletor)
- [ ] **Panic button** operacional (web + Telegram + CLI standalone) para zerar mitigações em emergência
- [ ] **Modo dry-run** e **modo manual-approve via Telegram inline buttons** como defaults antes de auto-mitigation

### Out of Scope

<!-- Boundaries explícitas com motivo. -->

- **Ataques application-layer (HTTP/HTTPS flood, Slowloris)** — vetor diferente, requer reverse-proxy/WAF; foco do MVP é volumétrico, que foi o incidente real
- **Scrubbing center / redirecionamento de tráfego** — adiciona infraestrutura cara e operação complexa; RTBH+Flowspec resolvem o MVP
- **SaaS multi-organização cobrado** — uso interno (ISP do amigo + meu); comercialização não é meta agora
- **Detecção via DPI (deep packet inspection)** — flows amostrados (sFlow/NetFlow) são suficientes e escaláveis para volumétrico
- **Mobile app nativo** — dashboard web + Telegram cobrem o caso de uso móvel

## Context

- **Incidente disparador:** ISP de um amigo ficou minutos fora por um ataque UDP/ICMP flood que **nenhum monitoramento existente detectou**. O ataque só foi percebido quando o link caiu. Esse é o pesadelo operacional que motiva o projeto.
- **Ambiente típico:** ISPs brasileiros pequenos e médios, com mistura de Mikrotik (RouterOS) e Juniper, alguns Cisco, todos rodando BGP com upstreams.
- **Referências conceituais:**
  - [`pavel-odintsov/fastnetmon`](https://github.com/pavel-odintsov/fastnetmon) — engine de detecção DDoS Apache 2.0, maduro (10 anos, usado AMS-IX/Pentanet). **Será incorporado como engine de detecção.**
  - [`lupael/ddos-potection`](https://github.com/lupael/ddos-potection) — projeto Python/FastAPI/React MIT que cobre o mesmo escopo, mas em stack incompatível com a escolhida (Python vs Go). Servirá como **referência conceitual de features**, não fork.
- **Stack escolhida:** **Go 1.22+** orquestrando **FastNetMon Community** (detecção) + **GoBGP v4.5+ embeddado** (mitigação) + **PostgreSQL** (incidentes/config/audit) + **Vue 3 + Vite + Naive UI** (dashboard com SSE) + **go-telegram/bot** + **wneessen/go-mail** + **systemd + goreleaser** (deploy como `.deb`/`.rpm`).
- **Limitações de vendor conhecidas (research-validated):**
  - Mikrotik RouterOS **não suporta sFlow** (só NetFlow v1/v5/v9 + IPFIX) — feature request aberto desde 2021.
  - Mikrotik RouterOS v7 **não suporta BGP Flowspec** — Flowspec apenas em Juniper/Cisco.
  - Mikrotik tem bug de byte-order em sampling rate de NetFlow v9 (RouterOS v6.49.6+) que causa erro de ~1000x em PPS — workaround conhecido (hard-code do sample rate no coletor).
  - Mikrotik RouterOS v7 tem bug onde flag de blackhole pode ficar presa na FIB após withdraw BGP — workaround via route-refresh periódico.
- **Padrão de ataque atual:** carpet-bombing (atacar /22 inteiro com baixa taxa por IP) é o vetor dominante 2024-2026 e fura detecção por IP isolado. Exige agregação multi-resolução desde o MVP.
- **Usuário direto:** operador de rede do ISP — alguém que entende BGP, lê alertas no celular e age rápido.

## Constraints

- **Tech stack**: **Go 1.22+** orquestrador + **FastNetMon Community** engine de detecção + **GoBGP v4.5+** speaker BGP embeddado + **Postgres 16** + **Vue 3 + Vite + Naive UI** + SSE para real-time + **systemd** deploy. Sem Docker em produção (operadores ISP preferem `.deb`/`.rpm`).
- **Timeline**: MVP sólido em **algumas semanas** — não dias (precisa ser confiável) nem meses (próximo ataque pode vir a qualquer momento).
- **Dependencies**: Roteadores precisam (a) suportar export NetFlow v9 ou IPFIX (sFlow opcional, Juniper/Cisco apenas) e (b) aceitar sessão BGP de mitigação dedicada. Sem isso, o produto não funciona.
- **Performance**: Latência sample→mitigação ≤ 7s. Deve ingerir flows sem perder pacotes em ISP de 1–10 Gbps. Hot path (counters últimos 60s) em RAM; cold path (incidentes) em Postgres.
- **Operacional**: Falsos positivos custam reputação — blackholar cliente legítimo é pior que perder ataque pequeno. **Dry-run e manual-approve são defaults**, auto-mitigation só é habilitado após confiança operacional.
- **BGP safety**: Sessão BGP de mitigação **dedicada** (separada da sessão de produção), com filtro saída estrito strippando community blackhole nas outras, origin check (refusar /32 fora do espaço próprio) e TTL obrigatório em toda announcement.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Mitigação via BGP (RTBH + Flowspec), **não scrubbing** | Scrubbing exige infra cara; RTBH/Flowspec funcionam com o que o ISP já tem | — Pending |
| Multi-tenant: ISP e cliente corporativo instalam **separadamente** | Cada um tem topologia, ASN e thresholds próprios; tentar multi-org compartilhado complicaria o MVP | — Pending |
| Foco em **volumétrico (UDP/ICMP + carpet-bombing)** primeiro | É o vetor do incidente real e o padrão dominante 2024-2026; app-layer pode entrar em v2 | — Pending |
| **Multi-vendor desde o início**: NetFlow v9 + IPFIX primary, sFlow opcional, RTBH+Flowspec por vendor | Ambiente heterogêneo do ISP exige flexibilidade; deteção de vendor por peer + escolha automática de mitigação | — Pending |
| Alertas **multi-canal desde o MVP** (Telegram + Email + Dashboard) | Operador precisa receber em qualquer lugar que estiver; Telegram domina ISPs brasileiros | — Pending |
| **Build greenfield em Go com FastNetMon como engine** (não fork de lupael) | Stack alinhada com ecossistema BGP/flow (GoBGP, GoFlow2); FastNetMon é o único OSS DDoS-specific maduro; lupael é Python/FastAPI/Docker e ainda pouco testado (2 stars). lupael serve como referência conceitual de features. | ✓ Decidido 2026-05-17 |
| **Carpet-bombing detection é P1** (multi-resolução /32, /28, /24, /22 no MVP) | Padrão dominante 2024-2026 fura thresholds por IP isolado; ignorar = ferramenta cega ao ataque real | ✓ Decidido 2026-05-17 |
| **Dry-run + manual-approve via Telegram inline buttons como defaults** | Falso-positivo blackhole destrói reputação permanentemente; auto-mitigation só após confiança operacional | ✓ Decidido 2026-05-17 |
| **Panic button obrigatório no MVP** (web + Telegram + CLI standalone) | Operador precisa de escape hatch quando engine misfira; CLI standalone funciona sem DB/web caso esses estejam comprometidos | ✓ Decidido 2026-05-17 |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-05-17 after research synthesis (stack decision, vendor scope, carpet-bombing P1)*
