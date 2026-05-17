# Roadmap: Mitigador

**Created:** 2026-05-17
**Granularity:** coarse (4 phases)
**Coverage:** 65/65 v1 requirements mapped
**Core value:** Tornar visível e mitigar automaticamente o ataque volumétrico que hoje só é descoberto quando o link já caiu.

## Phases

- [ ] **Phase 1: Observation Spine** — Operador ganha visibilidade real-time de ataques volumétricos via dashboard + Telegram, sem risco BGP.
- [ ] **Phase 2: BGP Mitigation with Safety Rails** — Operador mitiga ataques via RTBH com dry-run/manual-approve defaults, panic button e audit log.
- [ ] **Phase 3: Carpet-Bombing, Flowspec & Operator UX** — Operador detecta carpet-bombing, usa Flowspec onde suportado, configura tudo via UI multi-tenant.
- [ ] **Phase 4: Production Packaging** — Operador instala via `apt install mitigador` em produção com systemd e métricas Prometheus.

## Phase Details

### Phase 1: Observation Spine
**Goal**: Operador do ISP ganha visibilidade completa em tempo real de ataques volumétricos (UDP/ICMP flood per-host) através de dashboard web + alertas Telegram/email, sem qualquer ação BGP. É o "observação pura" que prova que a detecção funciona antes de qualquer risco de mitigação.
**Depends on**: Nothing (first phase)
**Requirements**: TELE-01, TELE-02, TELE-03, TELE-04, TELE-05, TELE-06, TELE-07, DETE-01, DETE-02, DETE-03, DETE-05, DETE-06, ALER-01, ALER-02, ALER-05, ALER-06, ALER-08, DASH-01, DASH-02, DASH-04, DASH-05, DASH-09, PERS-01, PERS-03, PERS-04
**Success Criteria** (what must be TRUE):
  1. Operador configura um roteador Mikrotik para exportar NetFlow v9 e vê flows chegando no dashboard em < 60s, com PPS/BPS corretos (apesar do bug de byte-order Mikrotik).
  2. Quando um IP cliente sofre ataque UDP flood ou ICMP flood acima do threshold do hostgroup, operador recebe alerta Telegram em pt-BR com IP alvo, vetor, taxa pps/bps e duração — **sem qualquer anúncio BGP**.
  3. Operador abre o dashboard web (login com sessão), vê ataques ativos em tempo real via SSE, e vê quais exporters estão saudáveis (último flow por fonte, taxa de chegada).
  4. Operador consulta histórico de incidentes detectados no PostgreSQL com retenção mínima de 1 ano, mesmo após reiniciar o serviço.
  5. Sistema rejeita flows de IPs de exporter não-cadastrados no inventário (sem inflar contadores com dados de fontes desconhecidas).
**Plans**: TBD
**UI hint**: yes

### Phase 2: BGP Mitigation with Safety Rails
**Goal**: Operador habilita mitigação BGP RTBH com todas as barreiras de segurança (whitelist, dry-run default, manual-approve via Telegram inline button, panic button, TTL obrigatório, origin check, audit log imutável). Mitigação real fica disponível mas falsos-positivos catastróficos são estruturalmente impossíveis.
**Depends on**: Phase 1
**Requirements**: MITI-01, MITI-02, MITI-05, MITI-06, MITI-07, MITI-08, MITI-09, MITI-10, SAFE-01, SAFE-02, SAFE-03, SAFE-04, SAFE-05, SAFE-06, SAFE-07, SAFE-08, ALER-03, ALER-04, DASH-08, PERS-02
**Success Criteria** (what must be TRUE):
  1. Na primeira inicialização da Phase 2, sistema está em **dry-run mode por default** — logs mostram "RTBH /32 X.X.X.X *would* be announced" mas nenhum BGP UPDATE sai. Mudança para auto exige confirmação explícita do operador.
  2. Em manual-approve mode, operador recebe alerta Telegram com inline buttons "Aprovar mitigação / Ignorar", clica Aprovar do celular (autorizado pelo chat ID), e vê dentro de ~7 segundos o RTBH /32 anunciado para o peer BGP de mitigação dedicado, com community correta do upstream e TTL configurado (auto-withdraw após N minutos).
  3. Operador clica o **panic button no dashboard mobile-friendly** (ou executa `/panic_stop_all` no Telegram, ou `mitigador panic --withdraw-all` no shell sem precisar de DB/web) e todas as mitigações ativas são retiradas em < 5 segundos.
  4. Sistema **se recusa a anunciar** /32 de um IP na whitelist (trusted-networks), /32 fora do espaço próprio (origin check), ou além do hard cap de mitigações concorrentes — falha audível, com motivo no audit log.
  5. Toda announcement BGP enviada (UPDATE/WITHDRAW) está no audit log append-only/imutável com timestamp, operador (se manual), motivo, e payload completo — queryável no dashboard. Após restart do serviço, mitigações ativas são reconciliadas a partir do banco.
**Plans**: TBD
**UI hint**: yes

### Phase 3: Carpet-Bombing, Flowspec & Operator UX
**Goal**: Operador detecta ataques de carpet-bombing (padrão dominante 2024-2026) via agregação multi-resolução /28/24/22, usa Flowspec automaticamente nos peers Juniper/Cisco (fallback RTBH em Mikrotik), e configura tudo pela UI (hostgroups, thresholds, peers, alert channels, whitelist). Deploy multi-tenant via systemd instantiated units com templates BR pré-carregados.
**Depends on**: Phase 2
**Requirements**: DETE-04, DETE-07, MITI-03, MITI-04, ALER-07, DASH-03, DASH-06, DASH-07, DASH-10, MTEN-01, MTEN-02, MTEN-03, MTEN-04, MTEN-05
**Success Criteria** (what must be TRUE):
  1. Quando atacante distribui 12 Mbps × 1000 IPs num /22 cliente (carpet-bombing) sem nenhum /32 individual triggar, sistema detecta via agregação em /28/24/22 e envia **uma única notificação agregada** ("12 IPs em /24 X.X.X.0/24 atacados, 8 Gbps UDP/53") em vez de N notificações individuais.
  2. Sistema detecta vendor de cada peer BGP no startup (capability detection) e quando dispara mitigação contra carpet-bombing num peer Juniper/Cisco, anuncia regra **Flowspec granular**; quando o peer é Mikrotik, faz **fallback automático para RTBH** sem operador intervir.
  3. Operador roda Mitigador em **modo baseline** por 1-2 semanas (observação, sem ação) e o sistema sugere thresholds por hostgroup baseado no tráfego observado, em vez de exigir tunagem manual cega.
  4. Operador faz **CRUD completo via dashboard** (hostgroups, thresholds, peers BGP, alert channels, whitelist) sem editar YAML, e filtra histórico de incidentes por data/vetor/IP/ação. Dashboard mostra attack timeline com gráfico pps/bps + top source ASNs.
  5. Operador executa `systemctl start mitigador@isp.service` e `systemctl start mitigador@cliente.service` em paralelo com configs, schemas DB e sessões BGP **completamente isolados**; templates de communities BGP brasileiros (Telxius/Lumen/Algar/V.tal/IX.br) e perfis de hostgroup (residencial/corporate/gaming/DNS) estão pré-carregados; sistema **recusa boot** se ASN do tenant não bate com a community configurada.
**Plans**: TBD
**UI hint**: yes

### Phase 4: Production Packaging
**Goal**: Operador instala Mitigador em produção como qualquer outro pacote Linux (`apt install mitigador` ou `dnf install mitigador`), edita `/etc/mitigador/config.yaml`, faz `systemctl start mitigador@isp` e está rodando — sem Docker, sem build local, sem ginástica. Métricas Prometheus disponíveis e documentação cobre Mikrotik + Juniper passo-a-passo.
**Depends on**: Phase 3
**Requirements**: OPER-01, OPER-02, OPER-03, OPER-04, OPER-05, OPER-06
**Success Criteria** (what must be TRUE):
  1. Operador num Debian/Ubuntu limpo executa `apt install ./mitigador_X.Y.Z_amd64.deb` (gerado pelo goreleaser no release do GitHub) e o serviço inicia via systemd sem dependências externas além de PostgreSQL.
  2. O mesmo release tem `.rpm` funcional para Rocky/RHEL, gerado da mesma pipeline de release.
  3. Documentação cobre passo-a-passo de configuração de **NetFlow + BGP peer** num Mikrotik real e **NetFlow/sFlow + BGP peer + Flowspec capability** num Juniper real — com snippets copy-paste testados.
  4. Endpoint `/metrics` expõe métricas Prometheus (flows recebidos por fonte, ataques detectados, mitigações ativas, latência detect→mitigate, saúde de sessões BGP) — sem exigir Prometheus como dependência.
**Plans**: TBD

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Observation Spine | 0/0 | Not started | - |
| 2. BGP Mitigation with Safety Rails | 0/0 | Not started | - |
| 3. Carpet-Bombing, Flowspec & Operator UX | 0/0 | Not started | - |
| 4. Production Packaging | 0/0 | Not started | - |

## Coverage Map

Total v1 requirements: **65**
Mapped to phases: **65** (100%)
Unmapped: **0**

### Phase 1 (25 requirements)
TELE-01, TELE-02, TELE-03, TELE-04, TELE-05, TELE-06, TELE-07, DETE-01, DETE-02, DETE-03, DETE-05, DETE-06, ALER-01, ALER-02, ALER-05, ALER-06, ALER-08, DASH-01, DASH-02, DASH-04, DASH-05, DASH-09, PERS-01, PERS-03, PERS-04

### Phase 2 (20 requirements)
MITI-01, MITI-02, MITI-05, MITI-06, MITI-07, MITI-08, MITI-09, MITI-10, SAFE-01, SAFE-02, SAFE-03, SAFE-04, SAFE-05, SAFE-06, SAFE-07, SAFE-08, ALER-03, ALER-04, DASH-08, PERS-02

### Phase 3 (14 requirements)
DETE-04, DETE-07, MITI-03, MITI-04, ALER-07, DASH-03, DASH-06, DASH-07, DASH-10, MTEN-01, MTEN-02, MTEN-03, MTEN-04, MTEN-05

### Phase 4 (6 requirements)
OPER-01, OPER-02, OPER-03, OPER-04, OPER-05, OPER-06

## Phase Ordering Rationale

- **Phase 1 é o "minimum viable observation"**: ataca a dor do incidente disparador (ataque invisível) com risco zero. Sem BGP = sem possibilidade de blackholar cliente errado. Prova que detecção funciona antes de qualquer mitigação.
- **Phase 2 concentra todo o risco operacional num único bloco controlado**: blackhole errado é o pesadelo #1 (Cloudflare 1.1.1.1 incident); Phase 2 inteira existe para tornar isso estruturalmente impossível via defaults conservadores (dry-run, manual-approve, whitelist, TTL, origin check) + escape hatch (panic button triplo) + accountability (audit log imutável).
- **Phase 3 traz o estado-da-arte** (carpet-bombing + Flowspec) depois que a base é confiável — ataques carpet-bombed são P1 mas só fazem sentido depois da espinha + safety. Operator UX (CRUD via UI, multi-tenant, templates BR) cabe aqui porque é o que torna o produto usável por outros ISPs.
- **Phase 4 é puro packaging** — código já roda em Phase 3 (binário Go + systemd), Phase 4 transforma isso em `.deb`/`.rpm` distribuível e documentação ISP-friendly.

## Deferred (v2+)

Documentado em REQUIREMENTS.md sob "v2 Requirements":
- SYN flood / DNS amp / NTP amp / entropy / ML detection
- IPv6 BGP (RTBH + Flowspec em AFI v6)
- Mikrotik API direta (push de firewall rules)
- Scrubbing center integration (DOTS)
- Relatórios mensais PDF/CSV, abuse reports automáticos
- RBAC multi-role, multi-org SaaS
- Webhooks genéricos (Slack/Discord/PagerDuty/SIEM)

---
*Last updated: 2026-05-17 (initial roadmap)*
