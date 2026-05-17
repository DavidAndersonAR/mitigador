# State: Mitigador

**Last updated:** 2026-05-17

## Project Reference

**Core value**: Tornar visível e mitigar automaticamente o ataque volumétrico que hoje só é descoberto quando o link já caiu. Se tudo mais falhar, o operador precisa receber o alerta e ver a rota de blackhole/flowspec sendo anunciada antes do cliente reclamar.

**Current focus**: Roadmap definido (4 fases, coarse). Próximo passo: planejar Phase 1 (Observation Spine) — ingestão NetFlow v9/IPFIX/sFlow, detecção UDP/ICMP flood per-host, dashboard read-only via SSE, alertas Telegram/email. **Sem BGP em Phase 1** — visibilidade pura.

## Current Position

**Phase**: Pre-Phase 1 (roadmap aprovado, aguardando planejamento)
**Plan**: None
**Status**: Roadmap created
**Progress**: [░░░░░░░░░░] 0/4 phases complete

## Performance Metrics

| Metric | Target | Current |
|--------|--------|---------|
| Plans approved on first try | ≥ 70% | n/a |
| Nodes that pass verification | ≥ 80% | n/a |
| Repair budget consumption | ≤ 40% | n/a |
| Latência sample→mitigação (Phase 2+) | ≤ 7s | n/a |

## Accumulated Context

### Key Decisions (from PROJECT.md, validated pela pesquisa)

- **Stack:** Go 1.22+ orquestrador + FastNetMon Community (engine de detecção) + GoBGP v4.5+ (BGP embedded) + PostgreSQL 16 + Vue 3 + Vite + Naive UI + SSE + systemd + goreleaser (`.deb`/`.rpm`).
- **No Docker em produção:** operadores ISP preferem `.deb`/`.rpm` via systemd. Docker apenas para dev/lab.
- **No fork de `lupael/ddos-potection`:** stack incompatível (Python/FastAPI/Docker). Serve como referência conceitual de features apenas. Build greenfield.
- **Multi-vendor desde o MVP:** NetFlow v9 + IPFIX primary (Mikrotik universal); sFlow opcional (Juniper/Cisco only — Mikrotik não suporta); RTBH universal; Flowspec apenas em peers Juniper/Cisco (fallback automático para RTBH em Mikrotik).
- **Carpet-bombing detection é P1:** agregação multi-resolução /32, /28, /24, /22 — padrão dominante 2024-2026 fura detecção por IP isolado.
- **Dry-run + manual-approve via Telegram inline buttons são defaults:** auto-mitigation só após confiança operacional. Falso-positivo blackhole destrói reputação.
- **Panic button MVP-mandatory:** web + Telegram + CLI standalone (CLI funciona sem DB/web).
- **Multi-tenant via systemd instantiated units:** `mitigador@isp.service`, `mitigador@cliente.service` — NÃO multi-org SaaS.
- **Phase ordering:** observação pura (Phase 1) → BGP com todos os safety rails (Phase 2) → carpet-bombing + Flowspec + UX (Phase 3) → packaging (Phase 4).

### Active Todos

- [ ] Aprovar roadmap
- [ ] `/gsd-plan-phase 1` — planejar Phase 1 (Observation Spine)

### Blockers / Open Questions (resolver durante planning)

- **Inventário real do ISP do amigo** (mix de vendors, modelos, versões RouterOS, topologia BGP, upstreams) — resolver antes ou durante Phase 1.
- **Status atual dos bugs Mikrotik** (RTBH withdrawal flag stuck em RouterOS v7 + NetFlow v9 byte-order em v6.49.6+) em versões latest — validar em lab durante Phase 1 e Phase 2.
- **Matriz de communities BGP** dos upstreams brasileiros (Telxius/Lumen/Algar/V.tal/IX.br) — preencher antes de Phase 2.
- **Aceitação Flowspec dos upstreams BR** — empirical, Phase 3.
- **Teto de features do FastNetMon Community vs Advanced** — resolver incrementalmente Phases 1-3; fallback documentado para Go puro com GoFlow2 se necessário.

### Recent Highlights

- **2026-05-17:** Roadmap criado. 4 fases (coarse), 65 v1 requirements 100% mapeados. Phase 1 é observação-only (sem BGP); Phase 2 concentra todo o risco BGP com safety rails completos.
- **2026-05-17:** Stack decidida em PROJECT.md após research synthesis (Go + FastNetMon + GoBGP + Vue + systemd).
- **2026-05-17:** Carpet-bombing detection promovida a P1 (não pode ser deferida — é o padrão dominante de ataque).

## Session Continuity

**Last session ended at**: Roadmap creation
**Next session should**: Review ROADMAP.md e ROADMAP coverage map; aprovar (ou ajustar); então rodar `/gsd-plan-phase 1` para decompor Phase 1 (Observation Spine) em plans executáveis.

**Open files / artifacts in flight**:
- `.planning/PROJECT.md` (atualizado 2026-05-17)
- `.planning/REQUIREMENTS.md` (v1 = 65 requirements, traceability atualizada com mapping de fases)
- `.planning/ROADMAP.md` (criado 2026-05-17, 4 fases coarse)
- `.planning/research/SUMMARY.md` + STACK.md + FEATURES.md + ARCHITECTURE.md + PITFALLS.md (research completa, HIGH confidence em tech, MEDIUM em normas BR)

---
*State initialized: 2026-05-17 at roadmap creation*
