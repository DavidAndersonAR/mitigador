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

- [ ] Ingestão de telemetria via **sFlow** (Mikrotik, Juniper)
- [ ] Ingestão de telemetria via **NetFlow v5/v9** (Cisco e legados)
- [ ] Ingestão de telemetria via **IPFIX** (equipamentos modernos)
- [ ] Detecção de ataques **volumétricos UDP flood**
- [ ] Detecção de ataques **volumétricos ICMP flood**
- [ ] Mitigação automática via **BGP RTBH** (blackhole de /32 atacado)
- [ ] Mitigação automática via **BGP Flowspec** (regras granulares)
- [ ] **Alertas Telegram** com detalhes do ataque (IP alvo, vetor, taxa)
- [ ] **Alertas por e-mail** com sumário de incidente
- [ ] **Dashboard web em tempo real** com ataques ativos e histórico
- [ ] **Modo multi-tenant**: instalação separada para ISP e cliente corporativo, com configurações independentes
- [ ] Suporte a roteadores **Mikrotik, Juniper e Cisco** (sessão BGP de mitigação)
- [ ] Configuração de **thresholds por prefixo/cliente** (não thresholds estáticos globais)
- [ ] **Histórico de ataques** persistido para análise post-mortem

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
- **Referência base:** [`lupael/ddos-protection`](https://github.com/lupael/ddos-protection) — usuário pretende **partir desse projeto e estender** (fork/base, não construir do zero). Decisão de fork pendente de avaliação técnica do código existente.
- **Stack consumidora típica:** Linux servers como coletores; possível uso de ferramentas estabelecidas (FastNetMon, pmacct, gobgp/exabgp) como building blocks.
- **Usuário direto:** operador de rede do ISP — alguém que entende BGP, lê alertas no celular e age rápido.

## Constraints

- **Tech stack**: A definir após avaliação do `lupael/ddos-protection`. Provavelmente Linux + coletor de flows + speaker BGP (gobgp/exabgp) + UI web + bot Telegram. Linguagem do core ainda não fixada.
- **Timeline**: MVP sólido em **algumas semanas** — não dias (precisa ser confiável) nem meses (próximo ataque pode vir a qualquer momento).
- **Dependencies**: Roteadores precisam (a) suportar export de pelo menos um protocolo de flow e (b) aceitar sessão BGP de mitigação. Sem isso, o produto não funciona.
- **Performance**: Deve ingerir flows em produção sem perder pacotes; latência de detecção precisa ser segundos (não minutos), senão o ataque já causou estrago.
- **Operacional**: Falsos positivos custam reputação — bloquear cliente legítimo via RTBH é pior que deixar passar um ataque pequeno. Threshold tuning e modo de aprovação manual são considerações importantes.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Mitigação via BGP (RTBH + Flowspec), **não scrubbing** | Scrubbing exige infra cara; RTBH/Flowspec funcionam com o que o ISP já tem | — Pending |
| Multi-tenant: ISP e cliente corporativo instalam **separadamente** | Cada um tem topologia, ASN e thresholds próprios; tentar multi-org compartilhado complicaria o MVP | — Pending |
| Foco em **volumétrico (UDP/ICMP)** primeiro | É o vetor do incidente real; app-layer pode entrar em v2 | — Pending |
| Aceitar **qualquer protocolo de flow** (sFlow + NetFlow + IPFIX) | Ambiente heterogêneo do ISP exige flexibilidade de input | — Pending |
| Alertas **multi-canal desde o MVP** (Telegram + Email + Dashboard) | Operador precisa receber em qualquer lugar que estiver; Telegram domina ISPs brasileiros | — Pending |
| Partir como **fork/base de lupael/ddos-protection** | Acelera MVP; reaproveita lógica já validada | — Pending (avaliar) |

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
*Last updated: 2026-05-17 after initialization*
