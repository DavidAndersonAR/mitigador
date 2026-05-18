# Phase 1: Observation Spine - Discussion Log

> **Audit trail apenas.** Não use como input para planning, research ou execution agents.
> Decisões canônicas estão em `01-CONTEXT.md` — este log preserva as alternativas consideradas.

**Date:** 2026-05-18
**Phase:** 01-observation-spine
**Mode:** discuss (interactive, pt-BR)
**Areas discussed:** Pipeline ingest/detect (greenfield Go), Bootstrap de configuração, Autenticação do dashboard, Ciclo de alerta + dedup

---

## Pivô crítico antes da discussão

O usuário corrigiu o entendimento inicial: **FastNetMon Community NÃO é runtime engine** — serve apenas como referência conceitual. Todo o detector é greenfield em Go puro dentro do binário Mitigador. Salvo em memory `project-detector-greenfield-go` para futuras sessões. Esse pivô inverte parte do que está documentado em `.planning/PROJECT.md`, `.planning/ROADMAP.md`, `.planning/STATE.md` e `.planning/research/STACK.md` — sinalizado em CONTEXT.md sob "Action Items para outros docs".

Após o pivô, a Área 1 foi reformulada de "Integração FastNetMon ↔ Mitigador" para "Pipeline de ingestão e detecção Go-nativa".

---

## Área 1: Pipeline de ingestão e detecção Go-nativa

### Q1.1 — Biblioteca de ingestão NetFlow v9 + IPFIX + sFlow v5

| Option | Description | Selected |
|--------|-------------|----------|
| GoFlow2 embedded como lib | Maduro (Cloudflare origin), v2.2.6 ativo, cobre v5/v9/IPFIX/sFlow, importa como módulo, producer customizado | ✓ |
| GoFlow2 como sidecar (Kafka) | Isola crash, mas adiciona Kafka como dep — over-engineered para MVP de ISP pequeno | |
| Implementação própria from scratch | Máximo controle, custo proibitivo (templates stateful, vendor quirks) | |

**Resultado:** GoFlow2 embedded como library.

### Q1.2 — Topologia dos UDP listeners (portas 2055/4739/6343)

| Option | Description | Selected |
|--------|-------------|----------|
| 1 goroutine por porta + worker pool | Simples, suficiente para 1-10 Gbps borders; SO_REUSEPORT só se medir contenção | ✓ |
| SO_REUSEPORT desde o MVP, M sockets por porta | Mais throughput, mais complexo de tunar; vale > 10 Gbps | |
| Decidir depois com base em load test | Punta a decisão; ainda válido para refinamento, mas pediu defaults agora | |

**Resultado:** 1 goroutine por porta + pool de workers desserializando.

### Q1.3 — Design dos counters per-host (sliding window 60s)

| Option | Description | Selected |
|--------|-------------|----------|
| Ring buffer 60×1s por IP, sharded por hash(IP) em N shards | Cada shard map[ip]*ringBuffer com mutex; escala linear com cores | ✓ |
| Single big map[IP]*ring com RWMutex global | Mais simples, gargalo com muitos hosts | |
| Counters in-band no detector (sem estrutura compartilhada) | Aggregator+detector em uma goroutine, snapshots via channel | |

**Resultado:** Ring buffer sharded por hash(IP).

### Q1.4 — Shape do AttackEvent e emissão started/updates/ended

| Option | Description | Selected |
|--------|-------------|----------|
| State machine por (IP, vetor): started → updates → ended com grace period | Janela mínima antes do started; updates com peak/avg; ended após grace | ✓ |
| Stream de attack-detected a cada tick + reduce a jusante | Detector simples, agregação no alerter | |
| Apenas started/ended (sem updates intermediários em P1) | Menos eventos; dashboard pega peak via API | |

**Resultado:** State machine started/updates/ended por (IP, vetor).

### Continuar?
"Próxima área (Bootstrap de config)". Itens deixados de fora: thresholds multi-criteria detalhe (DETE-05), classificação de vetor (DETE-06), gate de IP de exporter (TELE-05) — coberto inline em decisões/Claude's discretion no CONTEXT.

---

## Área 2: Bootstrap de configuração (sem CRUD UI)

### Q2.1 — Onde vive cada tipo de configuração

| Option | Description | Selected |
|--------|-------------|----------|
| Híbrido: YAML p/ infra; Postgres p/ domínio | YAML p/ DSN/secret/portas/Telegram/SMTP; Postgres p/ exporters/hostgroups/thresholds/alert_channels/whitelist; DB = fonte de verdade do domínio | ✓ |
| 100% YAML (single source of truth no MVP) | Tudo em YAML; Postgres só p/ incidentes/audit; exige migração em P3 | |
| 100% Postgres com CLI pra popular | Tudo no DB desde o MVP via CLI; obriga CLI completa em P1 | |

**Resultado:** Híbrido YAML/infra + Postgres/domínio.

### Q2.2 — Como o operador semeia a configuração inicial

| Option | Description | Selected |
|--------|-------------|----------|
| Edita YAML e roda `mitigador config sync` (CLI cobra) | Declarativo, idempotente, diffs no log | ✓ |
| YAML lido no startup (sem CLI explícita) | Magic auto-seed; risco de drift entre YAML e edits manuais no DB | |
| CLI puro: `mitigador exporter add ...`, `hostgroup add ...` | Verbose, ideal p/ Ansible/IaC; sem YAML | |

**Resultado:** YAML declarativo + `mitigador config sync` idempotente.

### Q2.3 — Hot reload ou restart?

| Option | Description | Selected |
|--------|-------------|----------|
| Reinicia (systemctl restart) por enquanto | MVP simples; counters resetam (limpo) | ✓ |
| Hot reload via SIGHUP (só domínio) | Sem dropar listeners/counters; cuidado de concorrência | |
| Hot reload automático via LISTEN/NOTIFY do Postgres | Futuro: CRUD UI emite NOTIFY; skip em P1 | |

**Resultado:** Restart em P1; hot reload entra em P3.

### Q2.4 — Templates de threshold default por perfil

| Option | Description | Selected |
|--------|-------------|----------|
| Sem defaults — operador define explícito por hostgroup | Coerente com MTEN-03 = P3 | ✓ |
| 1 template default (genérico) com valores conservadores | Hostgroups novos herdam; warning-only | |
| Modo baseline embed (antecipa DETE-07) | Sistema observa percentis; antecipa P3 | |

**Resultado:** Sem defaults; hostgroups precisam de threshold explícito.

### Continuar?
"Próxima área (Autenticação do dashboard)". Itens deixados de fora: schema detalhado do YAML, escopo do toggle pt-BR/en-US (DASH-09), credenciais Telegram/SMTP/chat-IDs (já implicitamente cobertos como "infra no YAML").

---

## Área 3: Autenticação do dashboard

### Q3.1 — Modelo de usuários

| Option | Description | Selected |
|--------|-------------|----------|
| Tabela `users` com N usuários desde o MVP | CRUD via CLI em P1, UI em P3 | ✓ |
| Single admin only — password no YAML/env (hashed) | Mais simples; precisa adaptar em P3 | |
| Tabela + admin seedado via YAML (híbrido) | Híbrido com bootstrap-via-sync | |

**Resultado:** Tabela `users` com CRUD via CLI.

### Q3.2 — Algoritmo de hash de senha

| Option | Description | Selected |
|--------|-------------|----------|
| bcrypt (cost ≥12) | Boring, OWASP-OK em 2026 | ✓ |
| argon2id (params OWASP 2026) | Mais futureproof, setup mais complexo | |
| Claude's discretion | — | |

**Resultado:** bcrypt cost ≥12.

### Q3.3 — Storage de sessão

| Option | Description | Selected |
|--------|-------------|----------|
| `alexedwards/scs` + `pgxstore` (Postgres) | Sessões sobrevivem a restart; cookie httpOnly+Secure+SameSite=Lax | ✓ |
| scs + memstore (in-memory) | Restart desloga todo mundo | |
| scs + cookiestore (signed cookie) | Stateless; revogação complicada | |

**Resultado:** scs + pgxstore.

### Q3.4 — Como criar o primeiro admin (bootstrap auth)

| Option | Description | Selected |
|--------|-------------|----------|
| CLI `mitigador user create` interativo (lê password do TTY) | Idempotente; sem segredo em arquivo | ✓ |
| Admin seedado pelo `mitigador config sync` (YAML c/ hash) | Declarativo, alinha com Bootstrap | |
| Env var `MITIGADOR_BOOTSTRAP_PASSWORD` | Inspirado em GitLab/Gitea; risco de leak | |

**Resultado:** CLI `mitigador user create` interativo.

### Continuar?
"Próxima área (Ciclo de alerta + dedup)". Itens deixados de fora: timeout de sessão, CSRF middleware, rate limit no endpoint de login (caem em Claude's discretion).

---

## Área 4: Ciclo de alerta + dedup

### Q4.1 — Cadência do Telegram em relação ao ciclo de vida do ataque

| Option | Description | Selected |
|--------|-------------|----------|
| On `started` + `ended` — 1 update intermediário só se peak >2× OU duração >5min | Pega início/fim sempre, update só se materialmente mudou | ✓ |
| Só on `started` + `ended` | Mínimo; operador na rua perde escala | |
| On `started` + update a cada N min fixo + `ended` | Tick previsível; spam em ataques longos | |

**Resultado:** Started + ended + 1 update condicional (peak >2× OU duração >5min).

### Q4.2 — Granularidade da deduplicação

| Option | Description | Selected |
|--------|-------------|----------|
| Chave (host_ip, vector) + grace 60s após `ended` antes de novo `started` | Bate com janela 60s | ✓ |
| Chave (host_ip) — vetores misturados no mesmo evento | 1 incidente enriquecido com lista de vetores | |
| Chave (host_ip, vector) sem grace — ended é ended | Operador vê flapping | |

**Resultado:** (host_ip, vector) + grace de 60s.

### Q4.3 — Cadência do email vs Telegram

| Option | Description | Selected |
|--------|-------------|----------|
| Email mesma cadência do Telegram (paridade total) | Cumpre ALER-05 "mesmo conteúdo + link" | ✓ |
| Email só `started` + `ended` (mais conservador que TG) | Email para registro/escalation/post-mortem | |
| Email só `ended` (sumário pós-fato) | Email vira relatório; Telegram para reagir | |

**Resultado:** Email pareado com Telegram (mesma cadência).

### Q4.4 — Resolução do DASH-04 (saúde de sessões BGP) em fase sem BGP

| Option | Description | Selected |
|--------|-------------|----------|
| P1 mostra card "Sessões BGP: nenhuma configurada" (stub vazio) | Resolve literalmente o requirement; código morto em P1 | ✓ |
| Mover DASH-04 para Phase 2 (atualizar roadmap/requirements) | Mais limpo; muda escopo | |
| Reescrever DASH-04 como "saúde de serviços externos" (genérico) | Postgres + ingestion + alert channels em P1; BGP em P2+ | |

**Resultado:** Stub vazio em P1; populado em P2. (Sinalizado em "Action Items" do CONTEXT que mover para Phase 2 numa próxima revisão é alternativa mais limpa.)

---

## Áreas exploradas adicionalmente?

Pergunta final: "Pronto pro CONTEXT.md ou explorar mais gray areas?"
**Resposta:** "Pronto pro CONTEXT.md."

---

## Claude's Discretion (delegado durante a discussão)

- Schema detalhado das tabelas Postgres e do YAML (estrutura, validação, exemplos).
- Política de timeout de sessão, CSRF, rate limit no login.
- Implementação concreta do workaround Mikrotik NetFlow v9 byte-order (semântica do `sample_rate_override` na tabela `exporters`).
- SSE event types, cadência, heartbeat.
- Implementação do rate-limit Telegram (ALER-08, 30 msg/s) — leaky/token bucket, queue in-memory ou DB.
- Estrutura de pastas Go.
- `internal/bgp/` em P1: stub ou vazio.
- Estratégia de servir o SPA Vue (embed `embed.FS` vs Nginx).
- Implementação concreta do toggle pt-BR/en-US (vue-i18n + chaves server-side).
- Estratégia de testes do pipeline sem hardware real (flow generator local).

## Deferred Ideas

- Carpet-bombing detection (DETE-04, DETE-07) → Phase 3.
- CRUD UI completo (DASH-06, 07, 10) → Phase 3.
- Hot reload de config (SIGHUP / LISTEN+NOTIFY) → Phase 3.
- Templates de threshold pré-carregados (MTEN-03) → Phase 3.
- Modo baseline (DETE-07) → Phase 3.
- Inline button approve no Telegram (ALER-03, 04) → Phase 2.
- Multi-tenant via systemd@.service (MTEN-01..05) → Phase 3.
- BGP completo (sessão, RTBH, Flowspec, safety, audit) → Phase 2/3.
- Popular DASH-04 com sessões reais → Phase 2.
- Mover DASH-04 do escopo de Phase 1 → próxima revisão de roadmap.

### Action items em outros docs (não fazem parte de Phase 1)
- Atualizar PROJECT.md, ROADMAP.md, STATE.md, research/STACK.md para refletir o pivô de FastNetMon-as-engine → FastNetMon-as-reference-only.
