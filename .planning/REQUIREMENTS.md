# Requirements: Mitigador

**Defined:** 2026-05-17
**Core Value:** Tornar visível e mitigar automaticamente o ataque volumétrico que hoje só é descoberto quando o link já caiu.

## v1 Requirements

Requirements para o release inicial. Cada um mapeia a uma fase do roadmap.

### Telemetria (Coleta de Flows)

- [ ] **TELE-01**: Sistema ingere NetFlow v9 via UDP/2055 e parseia templates corretamente (com cache persistente entre restarts)
- [ ] **TELE-02**: Sistema ingere IPFIX via UDP/4739 e parseia templates corretamente (com cache persistente)
- [ ] **TELE-03**: Sistema ingere sFlow v5 via UDP/6343 (para peers Juniper/Cisco que suportem)
- [ ] **TELE-04**: Sistema permite override de sampling rate por fonte (workaround para bug Mikrotik byte-order)
- [ ] **TELE-05**: Sistema valida IP de origem do exporter contra inventário configurado (rejeita flows de fontes desconhecidas)
- [ ] **TELE-06**: Sistema mantém contadores per-host (pps/bps por protocolo) em sliding window dos últimos 60s em memória
- [ ] **TELE-07**: Sistema mantém contadores agregados em múltiplos prefix lengths (/32, /28, /24, /22) para detecção de carpet-bombing

### Detecção

- [ ] **DETE-01**: Operador define thresholds por hostgroup (prefixo + tipo de cliente) — sem thresholds globais únicos
- [ ] **DETE-02**: Sistema detecta UDP flood per-host quando pps OU bps excede threshold por janela mínima configurável
- [ ] **DETE-03**: Sistema detecta ICMP flood per-host com mesma lógica multi-criteria
- [ ] **DETE-04**: Sistema detecta carpet-bombing quando agregação em /28, /24 ou /22 excede threshold mesmo sem /32 individual triggando
- [ ] **DETE-05**: Sistema atribui score de confiança ao evento de detecção (multi-criteria: pps AND bps AND duração)
- [ ] **DETE-06**: Sistema classifica o vetor do ataque (UDP flood / ICMP flood / carpet-bombing UDP / etc.) com base em proporção de protocolos
- [ ] **DETE-07**: Sistema oferece modo baseline (1-2 semanas) que observa tráfego e sugere thresholds — sem agir

### Mitigação BGP

- [ ] **MITI-01**: Sistema mantém sessão BGP dedicada de mitigação com peer configurável (separada de qualquer outra sessão)
- [ ] **MITI-02**: Sistema anuncia RTBH (/32) com community configurável por upstream — funciona para qualquer vendor
- [ ] **MITI-03**: Sistema anuncia Flowspec apenas para peers marcados como Juniper/Cisco; faz fallback a RTBH para Mikrotik
- [ ] **MITI-04**: Sistema escolhe automaticamente RTBH vs Flowspec baseado em capability detection do peer no startup
- [ ] **MITI-05**: Toda announcement tem TTL obrigatório (auto-withdraw após N minutos configurável)
- [ ] **MITI-06**: Sistema executa origin check — recusa anunciar /32 fora do espaço próprio (ASN owns prefix verification)
- [ ] **MITI-07**: Sistema aplica filtro de saída estrito strippando community blackhole em sessões BGP que não sejam a de mitigação
- [ ] **MITI-08**: Sistema executa route-refresh BGP periódico nos peers Mikrotik (workaround para bug de RTBH stuck)
- [ ] **MITI-09**: Sistema reconcilia estado BGP no startup (rebuilda mitigações ativas a partir do banco)
- [ ] **MITI-10**: Sistema verifica pós-withdraw se o tráfego cessou (cross-check com contadores) e alerta se não cessou

### Safety Rails (Segurança Operacional)

- [ ] **SAFE-01**: Operador configura whitelist / trusted-networks que **nunca** podem ser blackholed
- [ ] **SAFE-02**: Sistema oferece três modos: **dry-run** (não anuncia, só loga), **manual-approve** (espera aprovação via Telegram inline button), **auto** (anuncia direto)
- [ ] **SAFE-03**: Modo default na primeira inicialização é dry-run; mudança para auto exige confirmação explícita do operador
- [ ] **SAFE-04**: Sistema impõe hard cap de mitigações concorrentes (rate limit configurável)
- [ ] **SAFE-05**: Panic button no dashboard web zera todas as mitigações ativas em um clique (mobile-friendly)
- [ ] **SAFE-06**: Comando Telegram `/panic_stop_all` zera todas as mitigações ativas
- [ ] **SAFE-07**: CLI standalone `mitigador panic --withdraw-all` zera mitigações mesmo sem o serviço web/DB rodando
- [ ] **SAFE-08**: Sistema mantém audit log imutável (append-only) de todo BGP UPDATE enviado, com timestamp, operador (se aplicável), motivo e payload completo

### Alertas

- [ ] **ALER-01**: Operador configura bot Telegram + IDs de chat autorizados para receber alertas
- [ ] **ALER-02**: Alerta Telegram tem detalhes do ataque: IP alvo, vetor, taxa (pps/bps), duração, ação tomada (ou pedido de aprovação)
- [ ] **ALER-03**: Alerta Telegram em modo manual-approve tem inline buttons "✓ Aprovar mitigação" / "✗ Ignorar"
- [ ] **ALER-04**: Sistema verifica autorização do Telegram user que clicou o inline button (apenas IDs autorizados)
- [ ] **ALER-05**: Sistema envia alerta por e-mail (SMTP) com mesmo conteúdo + link para o incidente no dashboard
- [ ] **ALER-06**: Sistema deduplica alertas (não notifica o mesmo ataque a cada tick; agrupa em janela)
- [ ] **ALER-07**: Sistema agrega alertas de carpet-bombing em uma única notificação ("12 IPs em /24 atacados, 8 Gbps UDP/53") em vez de N notificações individuais
- [ ] **ALER-08**: Sistema respeita rate limit do Telegram (30 msg/s) e enfileira sem dropar mensagens

### Dashboard e API

- [ ] **DASH-01**: Operador faz login com credencial (sessão server-side, não JWT exposto)
- [ ] **DASH-02**: Dashboard mostra ataques ativos em tempo real via SSE (sem refresh manual)
- [ ] **DASH-03**: Dashboard mostra histórico de incidentes com filtros (data, vetor, IP alvo, ação)
- [ ] **DASH-04**: Dashboard mostra saúde das sessões BGP (estado, tempo desde último keepalive, mensagens trocadas)
- [ ] **DASH-05**: Dashboard mostra saúde dos exporters de flow (último flow recebido por fonte, taxa de chegada)
- [ ] **DASH-06**: Dashboard tem UI de CRUD para hostgroups, thresholds, peers BGP, alert channels, whitelist
- [ ] **DASH-07**: Dashboard tem visualização de attack timeline (gráfico pps/bps por ataque, top source ASNs)
- [ ] **DASH-08**: Dashboard tem audit log queryable (ver toda ação de mitigação histórica)
- [ ] **DASH-09**: UI em pt-BR (default) com possibilidade de toggle en-US
- [ ] **DASH-10**: API HTTP retorna estado atual (ativos, peers, exporters, métricas) em JSON para scripts/automação externa

### Multi-Tenant (Instalação Separada)

- [ ] **MTEN-01**: Sistema suporta deploy via systemd instantiated units (`mitigador@isp.service`, `mitigador@cliente.service`)
- [ ] **MTEN-02**: Cada instância tem config, DB schema e sessão BGP próprios — nenhum estado compartilhado entre tenants
- [ ] **MTEN-03**: Templates de config pré-carregados para perfis comuns (residencial, corporate, gaming, DNS server)
- [ ] **MTEN-04**: Templates de communities BGP pré-carregados para upstreams brasileiros principais (Telxius, Lumen, Algar, V.tal, IX.br)
- [ ] **MTEN-05**: Validação de config no startup recusa boot se houver mismatch (ex: ASN do tenant vs community)

### Persistência e Retenção

- [ ] **PERS-01**: Incidentes (sumário do ataque + ações tomadas) persistidos em PostgreSQL com retenção mínima de 1 ano
- [ ] **PERS-02**: Audit log de BGP UPDATEs persistido por no mínimo 1 ano (LGPD-friendly: append-only, imutável)
- [ ] **PERS-03**: Counters de flow em RAM (últimos 60s) — não persistidos em raw
- [ ] **PERS-04**: Sistema NÃO persiste flows individuais brutos no MVP (decisão consciente: storage explode; top-talkers cobre forensics)

### Deploy e Operacional

- [ ] **OPER-01**: Sistema distribuído como binário Go estático único + arquivos systemd + config exemplo
- [ ] **OPER-02**: Pacotes `.deb` (Debian/Ubuntu) e `.rpm` (Rocky/RHEL) gerados via goreleaser
- [ ] **OPER-03**: Instalação documentada: `apt install mitigador` (ou `dnf install mitigador`) + edição de `/etc/mitigador/config.yaml` + `systemctl start mitigador@isp`
- [ ] **OPER-04**: Sistema exporta métricas Prometheus em endpoint configurável (mas Prometheus não é dependência obrigatória)
- [ ] **OPER-05**: Documentação cobre passo-a-passo de configuração para Mikrotik (NetFlow + BGP peer)
- [ ] **OPER-06**: Documentação cobre passo-a-passo de configuração para Juniper (NetFlow/sFlow + BGP peer + Flowspec capability)

## v2 Requirements

Deferidos para release futuro. Conhecidos mas fora do MVP.

### Detecção Avançada

- **DETE-V2-01**: Detecção de SYN flood (TCP-específico)
- **DETE-V2-02**: Detecção de DNS amplification (UDP/53 com pattern específico)
- **DETE-V2-03**: Detecção de NTP amplification (UDP/123 com pattern específico)
- **DETE-V2-04**: Detecção baseada em entropy analysis (distribuição de source IPs)
- **DETE-V2-05**: Detecção ML-based (modelos de baseline learned)

### Mitigação Avançada

- **MITI-V2-01**: Suporte BGP para IPv6 (RTBH + Flowspec em AFI v6)
- **MITI-V2-02**: Integração direta Mikrotik API (push de firewall rules sem BGP)
- **MITI-V2-03**: Integração com scrubbing center externo (DOTS / signaling para upstream)

### Reporting

- **REPO-V2-01**: Relatórios mensais PDF/CSV exportáveis por tenant
- **REPO-V2-02**: Top talkers global e por incidente (origem do ataque)
- **REPO-V2-03**: Geração automática de abuse reports para upstreams
- **REPO-V2-04**: Dashboard executivo (SLA, ataques mês a mês, MTTD/MTTR)

### Operacional

- **OPER-V2-01**: RBAC com roles (admin, operator, viewer)
- **OPER-V2-02**: Multi-org SaaS (vários tenants em uma única instância com isolamento)
- **OPER-V2-03**: Webhook genérico para integração externa (Slack, Discord, PagerDuty, SIEM)
- **OPER-V2-04**: Integração com ticket system (Zammad, OTRS)

## Out of Scope

Excluídos explicitamente. Documentado para prevenir scope creep.

| Feature | Razão |
|---------|-------|
| Ataques application-layer (HTTP/HTTPS flood, Slowloris) | Vetor diferente; requer reverse-proxy/WAF; foco do MVP é volumétrico |
| Scrubbing center próprio | Infraestrutura cara; RTBH+Flowspec resolvem o MVP |
| Deep packet inspection (DPI) | Flows amostrados são suficientes para volumétrico; DPI não escala em borda ISP |
| Mobile app nativo | Telegram + dashboard web responsivo cobrem o caso móvel |
| PCAP capture completa de ataques | Storage explode (TB/dia); top-talkers cobre 95% das necessidades de forensics |
| SaaS multi-organização cobrado | Uso interno; comercialização não é meta agora (decisão em PROJECT.md) |
| Custom rule scripting (Lua/JS) | Adiciona attack surface e complexidade; templates fechados são suficientes para MVP |
| Detecção via inspeção de payload (signatures) | Volumétrico é detectado por volume, não por conteúdo |
| Integração com payment gateways | Não monetiza no MVP |
| Suporte a NetFlow v5 (legado puro) | Equipamentos modernos exportam v9/IPFIX; v5 é raro e tem campos limitados |

## Traceability

Mapeamento requirement → fase. Preenchido durante criação do roadmap em 2026-05-17.

| Requirement | Phase | Status |
|-------------|-------|--------|
| TELE-01 | Phase 1 | Pending |
| TELE-02 | Phase 1 | Pending |
| TELE-03 | Phase 1 | Pending |
| TELE-04 | Phase 1 | Pending |
| TELE-05 | Phase 1 | Pending |
| TELE-06 | Phase 1 | Pending |
| TELE-07 | Phase 3 | Pending |
| DETE-01 | Phase 1 | Pending |
| DETE-02 | Phase 1 | Pending |
| DETE-03 | Phase 1 | Pending |
| DETE-04 | Phase 3 | Pending |
| DETE-05 | Phase 1 | Pending |
| DETE-06 | Phase 1 | Pending |
| DETE-07 | Phase 3 | Pending |
| MITI-01 | Phase 2 | Pending |
| MITI-02 | Phase 2 | Pending |
| MITI-03 | Phase 3 | Pending |
| MITI-04 | Phase 3 | Pending |
| MITI-05 | Phase 2 | Pending |
| MITI-06 | Phase 2 | Pending |
| MITI-07 | Phase 2 | Pending |
| MITI-08 | Phase 2 | Pending |
| MITI-09 | Phase 2 | Pending |
| MITI-10 | Phase 2 | Pending |
| SAFE-01 | Phase 2 | Pending |
| SAFE-02 | Phase 2 | Pending |
| SAFE-03 | Phase 2 | Pending |
| SAFE-04 | Phase 2 | Pending |
| SAFE-05 | Phase 2 | Pending |
| SAFE-06 | Phase 2 | Pending |
| SAFE-07 | Phase 2 | Pending |
| SAFE-08 | Phase 2 | Pending |
| ALER-01 | Phase 1 | Pending |
| ALER-02 | Phase 1 | Pending |
| ALER-03 | Phase 2 | Pending |
| ALER-04 | Phase 2 | Pending |
| ALER-05 | Phase 1 | Pending |
| ALER-06 | Phase 1 | Pending |
| ALER-07 | Phase 3 | Pending |
| ALER-08 | Phase 1 | Pending |
| DASH-01 | Phase 1 | Pending |
| DASH-02 | Phase 1 | Pending |
| DASH-03 | Phase 3 | Pending |
| DASH-04 | Phase 1 | Pending |
| DASH-05 | Phase 1 | Pending |
| DASH-06 | Phase 3 | Pending |
| DASH-07 | Phase 3 | Pending |
| DASH-08 | Phase 2 | Pending |
| DASH-09 | Phase 1 | Pending |
| DASH-10 | Phase 3 | Pending |
| MTEN-01 | Phase 3 | Pending |
| MTEN-02 | Phase 3 | Pending |
| MTEN-03 | Phase 3 | Pending |
| MTEN-04 | Phase 3 | Pending |
| MTEN-05 | Phase 3 | Pending |
| PERS-01 | Phase 1 | Pending |
| PERS-02 | Phase 2 | Pending |
| PERS-03 | Phase 1 | Pending |
| PERS-04 | Phase 1 | Pending |
| OPER-01 | Phase 4 | Pending |
| OPER-02 | Phase 4 | Pending |
| OPER-03 | Phase 4 | Pending |
| OPER-04 | Phase 4 | Pending |
| OPER-05 | Phase 4 | Pending |
| OPER-06 | Phase 4 | Pending |

**Coverage:**
- v1 requirements: **65 total**
- Mapped to phases: **65 (100%)**
- Unmapped: **0**

**Per-phase counts:**
- Phase 1 (Observation Spine): 25 requirements
- Phase 2 (BGP Mitigation with Safety Rails): 20 requirements
- Phase 3 (Carpet-Bombing, Flowspec & Operator UX): 14 requirements
- Phase 4 (Production Packaging): 6 requirements

---
*Requirements defined: 2026-05-17*
*Last updated: 2026-05-17 after roadmap creation (traceability filled)*
