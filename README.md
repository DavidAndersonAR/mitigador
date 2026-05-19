# Mitigador

## What it is

Plataforma de detecção e mitigação automática de ataques DDoS volumétricos voltada para ISPs (provedores de internet) e clientes corporativos. Coleta telemetria de tráfego dos roteadores de borda (sFlow / NetFlow / IPFIX), identifica ataques que tipicamente passam despercebidos pelo monitoramento tradicional e responde automaticamente anunciando rotas de mitigação via BGP (RTBH e Flowspec) — tudo com alertas em tempo real por Telegram, e-mail e dashboard web.

**Core Value:** Tornar visível e mitigar automaticamente o ataque volumétrico que hoje só é descoberto quando o link já caiu. Se tudo mais falhar, o operador precisa receber o alerta e ver a rota de blackhole/flowspec sendo anunciada antes do cliente reclamar.

## Status

Phase 1 (Observation Spine) — em desenvolvimento. Sem código BGP em produção ainda.

## Quick start (dev)

```
go build -o mitigador ./cmd/mitigador
./mitigador --help
./mitigador version
```

## Project layout

| Path | Description |
|------|-------------|
| `cmd/mitigador/` | Cobra CLI entrypoint — root command and subcommand stubs |
| `internal/` | All application packages (config, flow, ingest, aggregate, detect, incident, alert, api, user, session, storage/postgres, bgp, version) |
| `migrations/` | SQL migration files embedded by `internal/storage/postgres` (plan 01-02) |
| `web/` | Vue 3 + Vite SPA source (plan 01-11) |
| `deploy/` | Operator-facing artifacts: `examples/config.yaml` and `systemd/mitigador.service` |

## License

TBD
