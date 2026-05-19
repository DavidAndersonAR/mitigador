---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
status: Ready to plan
last_updated: "2026-05-19T13:01:40.831Z"
progress:
  total_phases: 4
  completed_phases: 1
  total_plans: 12
  completed_plans: 12
  percent: 100
---

# State: Mitigador

**Last updated:** 2026-05-18

## Project Reference

**Core value**: Tornar visível e mitigar automaticamente o ataque volumétrico que hoje só é descoberto quando o link já caiu. Se tudo mais falhar, o operador precisa receber o alerta e ver a rota de blackhole/flowspec sendo anunciada antes do cliente reclamar.

**Current focus**: Phase 1 context capturado (`.planning/phases/01-observation-spine/01-CONTEXT.md`). Próximo passo: `/gsd-plan-phase 1` para decompor em plans executáveis. **Pivô importante registrado**: detection engine é greenfield em Go puro (FastNetMon = referência conceitual apenas, não runtime dep).

## Current Position

Phase: 2
Plan: Not started
**Phase**: Phase 1 — Observation Spine (context capturado, aguardando planning)
**Plan**: None
**Status**: Phase 1 CONTEXT.md gravado
**Progress**: [░░░░░░░░░░] 0/4 phases complete

## Performance Metrics

| Metric | Target | Current |
|--------|--------|---------|
| Plans approved on first try | ≥ 70% | n/a |
| Nodes that pass verification | ≥ 80% | n/a |
| Repair budget consumption | ≤ 40% | n/a |
| Latência sample→mitigação (Phase 2+) | ≤ 7s | n/a |

## Accumulated Context

### Roadmap Evolution

- Phase 01.1 inserted after Phase 1 (2026-05-19): Flow Analytics — top talkers + per-host charts (URGENT — operator drill-down need surfaced during Phase 1 smoke test)

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

- **2026-05-18:** Phase 1 CONTEXT.md gravado (`.planning/phases/01-observation-spine/01-CONTEXT.md`). 18 decisões implementacionais capturadas via `/gsd-discuss-phase 1`.
- **2026-05-18:** **Pivô FastNetMon:** detector é greenfield em Go puro (GoFlow2 embedded como lib). FastNetMon vira referência conceitual, não runtime dep. Implica atualizar PROJECT.md/ROADMAP.md/STACK.md numa próxima sessão de manutenção de planning.
- **2026-05-18:** DASH-04 (BGP health) em P1 fica como stub vazio até P2 popular; alternativa de mover para P2 documentada como follow-up.
- **2026-05-17:** Roadmap criado. 4 fases (coarse), 65 v1 requirements 100% mapeados. Phase 1 é observação-only (sem BGP); Phase 2 concentra todo o risco BGP com safety rails completos.
- **2026-05-17:** Stack decidida em PROJECT.md após research synthesis (Go + GoBGP + Vue + systemd).
- **2026-05-17:** Carpet-bombing detection: confirmada como **Phase 3** (DETE-04, DETE-07 mapeados a P3); P1 cobre apenas detecção per-host /32.

## Session Continuity

**Last session ended at**: Phase 1 context capture (`/gsd-discuss-phase 1`)
**Next session should**: Rodar `/gsd-plan-phase 1` para decompor Phase 1 em plans executáveis usando `.planning/phases/01-observation-spine/01-CONTEXT.md` como base. Considerar também a sessão de manutenção que atualize PROJECT.md/ROADMAP.md/STACK.md para o pivô greenfield Go (FastNetMon = referência).

**Open files / artifacts in flight**:

- `.planning/phases/01-observation-spine/01-CONTEXT.md` (criado 2026-05-18 — 18 decisões para o planner)
- `.planning/phases/01-observation-spine/01-DISCUSSION-LOG.md` (audit trail da discussão)
- `.planning/PROJECT.md` (atualizado 2026-05-17 — trecho sobre FNM como engine está obsoleto, ver CONTEXT)
- `.planning/REQUIREMENTS.md` (v1 = 65 requirements, traceability atualizada com mapping de fases)
- `.planning/ROADMAP.md` (criado 2026-05-17, 4 fases coarse — menções a FNM como engine obsoletas)
- `.planning/research/SUMMARY.md` + STACK.md + FEATURES.md + ARCHITECTURE.md + PITFALLS.md (research completa, HIGH confidence em tech, MEDIUM em normas BR — STACK.md sobre FNM obsoleto)

---
*State initialized: 2026-05-17 at roadmap creation*
