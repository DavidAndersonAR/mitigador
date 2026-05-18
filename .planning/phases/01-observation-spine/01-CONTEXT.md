# Phase 1: Observation Spine - Context

**Gathered:** 2026-05-18
**Status:** Ready for planning

<domain>
## Phase Boundary

Operador do ISP ganha **observação pura** de ataques volumétricos UDP/ICMP per-host em tempo real, sem qualquer ação BGP. Entrega:

- Ingestão NetFlow v9 / IPFIX / sFlow v5 com cadastro de exporters autorizados (TELE-01..05).
- Contadores per-host em sliding window 60s, agregação só no nível /32 (carpet-bombing fica para Phase 3) (TELE-06..07 parcial — /32 only em P1).
- Detecção de UDP flood e ICMP flood per-host com thresholds por hostgroup, score de confiança multi-criteria, e classificação de vetor (DETE-01..03, DETE-05, DETE-06).
- Alertas Telegram (com chat IDs autorizados) e e-mail SMTP em pt-BR, com dedup, com respeito ao rate-limit do Telegram (ALER-01, 02, 05, 06, 08).
- Dashboard web autenticado, em pt-BR (toggle en-US), mostrando ataques ativos em tempo real via SSE e saúde dos exporters (DASH-01, 02, 04 stub, 05, 09).
- Persistência de incidentes em Postgres com retenção mínima 1 ano; nada de flow record bruto persistido (PERS-01, 03, 04).

Fora do escopo desta phase (alocado a outras):
- Qualquer anúncio BGP (RTBH, Flowspec) e safety rails relacionados — Phase 2.
- Carpet-bombing, agregação multi-resolução, CRUD UI, modo baseline, multi-tenant — Phase 3.
- Packaging `.deb`/`.rpm`, métricas Prometheus, docs Mikrotik/Juniper — Phase 4.

</domain>

<decisions>
## Implementation Decisions

### Architecture Foundation
- **D-01:** Detection engine implementado **greenfield em Go puro** dentro do binário Mitigador. **FastNetMon Community NÃO é dependência de runtime** — serve apenas como referência conceitual (mesmo papel de `lupael/ddos-protection`). Esta decisão revisa o que está descrito em PROJECT.md, ROADMAP.md, STATE.md e `research/STACK.md` (ver "Action items para outros docs" em Deferred).

### Pipeline de ingestão e detecção
- **D-02:** Ingestão via **GoFlow2 (`netsampler/goflow2/v2`)** embedded como library. Cobre NetFlow v9 + IPFIX + sFlow v5. Producer customizado pluga no pipeline interno do Mitigador, sem rodar binário separado nem Kafka. (TELE-01..03)
- **D-03:** Topologia de UDP listeners: **1 goroutine `ReadFrom` por porta** (2055/4739/6343) + channel buffered + pool de workers desserializando em paralelo. SO_REUSEPORT só após load test demonstrar contenção.
- **D-04:** Per-host counters em sliding window 60s implementados como **ring buffer 60×1s por IP, sharded por hash(IP) em N shards** (N = `runtime.NumCPU()` por default). Cada shard com seu próprio mutex. Tick global de 1s avança head e expira buckets. (TELE-06)
- **D-05:** Detector emite `AttackEvent` com **state machine `started → updates → ended`** por chave `(host_ip, vector)`. `started` quando threshold violado por janela mínima (anti-flicker). `updates` periódicos com peak/avg. `ended` após grace period sem violação. (DETE-02, 03, 05, 06)
- **D-06:** Validação de exporter (TELE-05) feita no gate de ingestão antes de qualquer counter update: pacote UDP de IP fora do inventário é descartado, com log de evento (rate-limited).

### Bootstrap de configuração
- **D-07:** Configuração **híbrida**:
  - **YAML** em `/etc/mitigador/config.yaml` para infra: Postgres DSN, secret de sessão, portas HTTP/UDP, listen IPs, Telegram bot token + chat IDs autorizados, SMTP creds, `app_base_url`.
  - **Tabelas Postgres** para domínio: `exporters` (com sample rate override por fonte — TELE-04), `hostgroups`, `thresholds`, `alert_channels`, `whitelist` (esta usada só a partir de P2, mas cria já em P1). Fonte da verdade do domínio = DB.
  - Tabelas prontas para receber CRUD UI em Phase 3 (DASH-06).
- **D-08:** Seed inicial via CLI declarativa: `mitigador config sync` lê YAML opcional de domínio (separado do infra), faz **upsert idempotente** no DB e mostra diffs no log. Operador pode rodar várias vezes.
- **D-09:** **Sem hot reload em Phase 1.** Mudanças exigem `systemctl restart mitigador`. Listeners UDP reabrem, counters resetam (estado limpo). Hot reload entra em Phase 3 junto com CRUD UI.
- **D-10:** **Sem templates de threshold default em Phase 1.** Hostgroups novos precisam de threshold explícito do operador para detectar. Templates pré-carregados (residencial/corporate/gaming/DNS) são MTEN-03, Phase 3.

### Autenticação do dashboard
- **D-11:** Tabela **`users`** desde o MVP (colunas: id, username UNIQUE, password_hash, email, created_at, last_login). CRUD via CLI em P1 (`mitigador user create | list | passwd | delete`); UI em Phase 3.
- **D-12:** **Hash de senha bcrypt** com cost ≥12 (`golang.org/x/crypto/bcrypt`).
- **D-13:** Sessões via **`alexedwards/scs/v2` + `pgxstore`** (Postgres). Cookies `httpOnly` + `Secure` + `SameSite=Lax`. Sessões sobrevivem a restart; revogação e audit grátis.
- **D-14:** Primeiro admin via **`mitigador user create <username>`** (interativo, lê password do TTY, idempotente — erro se user já existe). Sem env var de bootstrap, sem password em YAML.

### Ciclo de alerta + dedup
- **D-15:** Telegram dispara em **`started` e `ended` sempre**, e **1 update intermediário** somente se `peak > 2× peak_inicial` **OU** `duração > 5 min` (o que ocorrer primeiro). Reduz spam sem cegar o operador em ataques longos. (ALER-02, 06)
- **D-16:** Dedup com chave **`(host_ip, vector)`** + grace de 60s após `ended` antes que novo `started` para o mesmo par seja considerado novo incidente. Bate com a janela de detection 60s.
- **D-17:** Email (SMTP) tem **mesma cadência do Telegram** (paridade total — cumpre ALER-05 "mesmo conteúdo + link para o incidente no dashboard"). Cada disparo Telegram gera 1 email correspondente.
- **D-18:** **DASH-04 em Phase 1:** componente de UI "Sessões BGP" existe no dashboard mas mostra o estado vazio com a mensagem **"Nenhuma sessão BGP configurada"**. Phase 2 popula com sessões reais. Resolve o requirement sem mover de fase. (Nota: numa próxima revisão de roadmap pode valer mover DASH-04 para Phase 2 — ver Deferred.)

### Claude's Discretion
Decisões deixadas para o planner / executor:
- Schema Postgres detalhado (tabelas `incidents`, `attack_updates`, `exporters`, `hostgroups`, `thresholds`, `users`, `sessions`, `alert_channels`, `whitelist`). Migrations via `golang-migrate/migrate/v4`.
- Schema YAML completo (campos, validação, exemplos).
- Política de timeout de sessão, middleware CSRF, rate limit no endpoint de login.
- Implementação concreta do workaround **Mikrotik NetFlow v9 byte-order** (TELE-04) — sample rate override por exporter já decidido como campo da tabela `exporters`; semântica do override fica livre.
- SSE event types, cadência, heartbeat/keepalive.
- Implementação do rate-limit Telegram (ALER-08, 30 msg/s) — leaky bucket ou token bucket, queue in-memory ou em Postgres, reconciliação após restart.
- Estrutura de pastas Go (`cmd/`, `internal/`) — usar como referência o esqueleto em `.planning/research/ARCHITECTURE.md` §"Recommended Project Structure" e ajustar conforme necessário.
- Pacote `internal/bgp/` em P1: pode ficar sem código, ou ter só a interface vazia. Decisão do planner.
- Estratégia de servir o SPA Vue (embed `embed.FS` no binário Go vs assets via Nginx vs binding direto na própria API com `chi`).
- Implementação concreta do toggle pt-BR/en-US (DASH-09): vue-i18n no front + chaves de mensagens no back para alertas operacionais (que ficam sempre em pt-BR, não dependem do toggle do dashboard).
- Estratégia de testes (unit/integration/E2E para o pipeline de ingestão e detecção sem hardware real — flow generator local).

### Folded Todos
Nenhum — `gsd-tools.cjs todo match-phase 1` retornou lista vazia.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents (researcher / planner / executor) MUST read these before research, planning, or implementation.**

### Roadmap e Requirements (fonte autoritativa de escopo)
- `.planning/ROADMAP.md` §"Phase 1: Observation Spine" — goal, depends-on, requirements oficiais da phase, success criteria, UI hint.
- `.planning/REQUIREMENTS.md` §Telemetria, §Detecção, §Alertas, §Dashboard e API, §Persistência e Retenção — texto canônico de TELE-01..07, DETE-01,02,03,05,06, ALER-01,02,05,06,08, DASH-01,02,04,05,09, PERS-01,03,04.
- `.planning/PROJECT.md` — visão, constraints, key decisions. **Atenção:** trechos que descrevem FastNetMon Community como engine de detecção estão obsoletos após o pivô documentado em D-01.

### Research (já produzida, fonte de patterns e gotchas)
- `.planning/research/STACK.md` — escolhas de stack validadas (Go 1.22+, chi v5, pgx v5, GoFlow2 v2.2.6, scs v2 + pgxstore, slog stdlib, `go-telegram/bot`, `wneessen/go-mail`, etc.). **Atenção:** trechos que tratam FastNetMon Community como engine estão obsoletos (ver D-01); o caminho oficial agora é GoFlow2 + custom detector em Go.
- `.planning/research/ARCHITECTURE.md` §"Recommended Project Structure" — esqueleto `cmd/mitigador`, `internal/ingest/{sflow,netflow,ipfix,normalize}`, `internal/aggregate`, `internal/detect`, `internal/api`, `internal/alert/{telegram,email}`, `internal/storage/postgres`.
- `.planning/research/ARCHITECTURE.md` §"Pattern 1: Pipeline with In-Process Channels (MVP)" — modelo prescritivo para o pipeline interno.
- `.planning/research/ARCHITECTURE.md` §"Pattern 4: Hot Path / Cold Path Split" — confirma RAM-only counters + Postgres só para incidentes (alinha com PERS-03, 04).
- `.planning/research/ARCHITECTURE.md` §"Data Flow → Detection Flow" — pipeline de referência (router → UDP → decode → aggregate → detect → fan-out).
- `.planning/research/ARCHITECTURE.md` §"Anti-Patterns → Persisting Raw Flow Records" — confirma PERS-04.
- `.planning/research/PITFALLS.md` — armadilhas conhecidas: byte-order de sampling rate em Mikrotik NetFlow v9, template cache stateful, UDP buffer overflow, etc.
- `.planning/research/FEATURES.md` — feature breakdown e referência conceitual de `pavel-odintsov/fastnetmon` (engine alheio que serve apenas como inspiração).
- `.planning/research/SUMMARY.md` — síntese cross-cutting da pesquisa.

### Estado e contexto
- `.planning/STATE.md` — accumulated context (key decisions, blockers, último status).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
Projeto **greenfield** — sem código no repo. Bibliotecas externas que serão alavancadas em Phase 1:

- **Ingestão:** `netsampler/goflow2/v2` (NetFlow v9, IPFIX, sFlow v5).
- **HTTP/SSE:** `go-chi/chi/v5` + middleware stdlib.
- **DB:** `jackc/pgx/v5` (driver direto, sem `database/sql`), `golang-migrate/migrate/v4` (migrations), `sqlc` (opcional, gerador de queries type-safe).
- **Sessões:** `alexedwards/scs/v2` + `alexedwards/scs/pgxstore`.
- **Auth/hash:** `golang.org/x/crypto/bcrypt`.
- **Alertas:** `go-telegram/bot` (Telegram), `wneessen/go-mail` (SMTP).
- **Config & CLI:** `spf13/viper` (YAML/env), `spf13/cobra` (subcommands: `serve`, `config sync`, `user create`, etc.).
- **Logs:** stdlib `log/slog`.
- **Métricas (Phase 4, mas exportável já em P1):** `prometheus/client_golang`.
- **IDs:** `oklog/ulid/v2` para incident IDs.
- **BGP:** `osrg/gobgp/v3` — fica para Phase 2; pacote `internal/bgp/` em P1 pode ficar vazio ou só com interface.

### Established Patterns
Nenhum padrão estabelecido (repo sem código). Pontos de partida:
- Estrutura `cmd/` + `internal/` proposta em `research/ARCHITECTURE.md` §"Recommended Project Structure".
- Pipeline em canais Go in-process (Pattern 1 do mesmo doc).
- Hot/cold path split (Pattern 4): RAM-only counters, Postgres só para incidentes.

### Integration Points
- **Roteadores de borda → portas UDP:** 2055 (NetFlow v9), 4739 (IPFIX), 6343 (sFlow v5). Exporters cadastrados via tabela `exporters` (IP + tipo + sample_rate_override opcional).
- **Postgres 16 (single DB do tenant em P1).** Multi-DB por systemd instance é Phase 3.
- **Telegram Bot API 9.5** — outgoing apenas em P1. Inline button callbacks (manual-approve) são Phase 2.
- **SMTP relay** configurável (STARTTLS/SMTPS), via `wneessen/go-mail`.
- **Dashboard SPA** (Vue 3 + Vite + Naive UI) — entrega via embed `embed.FS` no binário Go ou estática via Nginx (Claude's discretion).

</code_context>

<specifics>
## Specific Ideas

- Core value (citação do PROJECT.md): "Tornar visível e mitigar automaticamente o ataque volumétrico que hoje só é descoberto quando o link já caiu" — Phase 1 entrega só a parte de "tornar visível".
- Operador típico: engenheiro de rede de ISP brasileiro, leitor de Telegram, age do celular. Alertas em **pt-BR** (sempre, independente do toggle de UI). UI também em pt-BR por default, com toggle para en-US (DASH-09).
- Mikrotik é a baseline universal (NetFlow v9 + IPFIX); sFlow só para Juniper/Cisco onde existir. Já antecipar o byte-order workaround em P1 (TELE-04 — sample rate override por exporter).

</specifics>

<deferred>
## Deferred Ideas

Capturados durante a discussão, mas explicitamente fora de Phase 1:

- **Carpet-bombing detection** (agregação /28 /24 /22) — DETE-04, DETE-07 → Phase 3.
- **CRUD UI completo** (hostgroups, thresholds, peers, alert channels, whitelist) — DASH-06 → Phase 3.
- **Attack timeline + top source ASNs** — DASH-07, DASH-10 → Phase 3.
- **Hot reload de config (SIGHUP ou LISTEN/NOTIFY)** — Phase 3, junto com CRUD UI.
- **Templates de threshold pré-carregados** (residencial / corporate / gaming / DNS) — MTEN-03 → Phase 3.
- **Modo baseline (DETE-07)** que observa 1-2 semanas e sugere thresholds — Phase 3.
- **Inline button approve no Telegram** (ALER-03, ALER-04) — Phase 2 (parte do manual-approve BGP).
- **Multi-tenant via systemd instantiated units** — MTEN-01..05 → Phase 3.
- **BGP completo** (sessão dedicada, RTBH, Flowspec, panic button, audit log imutável, origin check, TTL) — Phase 2 / 3.
- **Popular DASH-04 com sessões BGP reais** — Phase 2. (Em P1 fica como stub vazio — D-18.)
- **Mover DASH-04 do escopo da Phase 1 para Phase 2** — alternativa mais limpa que o stub; pode ser feito numa próxima revisão de roadmap (não bloqueia P1).

### Action Items para outros docs (não é trabalho de Phase 1, sinalizado para próxima sessão de manutenção de planning)
- `.planning/PROJECT.md` — atualizar Tech Stack e Key Decisions removendo "FastNetMon Community como engine de detecção"; manter como referência conceitual (mesmo status de `lupael/ddos-protection`).
- `.planning/ROADMAP.md` — revisar menções a FNM nos goals/success criteria.
- `.planning/STATE.md` §"Key Decisions" — remover linha sobre FNM como engine; adicionar linha sobre detector greenfield em Go puro.
- `.planning/research/STACK.md` — marcar a recomendação histórica de FNM Community; promover GoFlow2 + custom Go detector como caminho oficial.
- `.planning/ROADMAP.md` — considerar mover DASH-04 para Phase 2 (alinhado com D-18).

</deferred>

---

*Phase: 01-observation-spine*
*Context gathered: 2026-05-18*
