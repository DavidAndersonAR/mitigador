# Mitigador

## What it is

Plataforma de detecção e mitigação automática de ataques DDoS volumétricos voltada para ISPs (provedores de internet) e clientes corporativos. Coleta telemetria de tráfego dos roteadores de borda (sFlow / NetFlow / IPFIX), identifica ataques que tipicamente passam despercebidos pelo monitoramento tradicional e responde automaticamente anunciando rotas de mitigação via BGP (RTBH e Flowspec) — tudo com alertas em tempo real por Telegram, e-mail e dashboard web.

**Core Value:** Tornar visível e mitigar automaticamente o ataque volumétrico que hoje só é descoberto quando o link já caiu. Se tudo mais falhar, o operador precisa receber o alerta e ver a rota de blackhole/flowspec sendo anunciada antes do cliente reclamar.

## Status

Phase 1 (Observation Spine) — complete. Full pipeline: NetFlow/IPFIX/sFlow → per-host counters → detection → Telegram/email alerts + SSE dashboard. No BGP mitigation yet (Phase 2).

## Development

### Prerequisites

- Go 1.22+ (`/home/david/tools/go/bin/go` or system Go)
- Node.js + pnpm (for dashboard SPA)
- PostgreSQL 16

### Build

```bash
# 1. Build the SPA (required before `go build` — Go embeds the Vite output)
cd web && pnpm install --frozen-lockfile && pnpm build && cd ..

# 2. Build the Go binary
go build -o mitigador ./cmd/mitigador
./mitigador --help
```

### Lab quick start (with synthetic traffic)

```bash
# 1. Create database
createdb mitigador

# 2. Configure: copy and edit the example configs
cp deploy/examples/config.yaml /tmp/config.yaml
cp deploy/examples/domain.yaml /tmp/domain.yaml
# Edit /tmp/config.yaml: set postgres.dsn, http.session_secret, telegram.*, smtp.*
# Edit /tmp/domain.yaml: set exporter source_ip to 127.0.0.1 for flowgen testing

# 3. Apply domain config (creates exporters, hostgroups, thresholds tables)
./mitigador --config /tmp/config.yaml config sync --file /tmp/domain.yaml

# 4. Create a dashboard user
./mitigador --config /tmp/config.yaml user create admin

# 5. Start the daemon
./mitigador --config /tmp/config.yaml serve

# 6. In another terminal: generate synthetic NetFlow v9 traffic
go run ./cmd/flowgen \
  --target=127.0.0.1:2055 \
  --src=127.0.0.1 \
  --dst=192.0.2.10 \
  --pps=100 \
  --bytes=200 \
  --duration=30s \
  --proto=17

# 7. Open http://localhost:8080 (or HTTPS via reverse proxy — Cookie.Secure=true)
#    Login as admin, watch the dashboard for the UDP flood incident.
```

### flowgen — synthetic NetFlow v9 generator

`cmd/flowgen` is a development-only tool that emits NetFlow v9 datagrams to a
local mitigador instance. It sends a template FlowSet first (required) then data
FlowSets at the configured interval.

```bash
go run ./cmd/flowgen --help
go run ./cmd/flowgen \
  --target=127.0.0.1:2055 \
  --src=10.0.0.1 \
  --dst=192.0.2.10 \
  --pps=100000 \
  --bytes=1000 \
  --duration=30s \
  --proto=17    # 17=UDP, 1=ICMP, 6=TCP
```

flowgen is NOT included in release builds (goreleaser excludes `cmd/flowgen`).

### Running tests

```bash
# Unit tests (no DB required)
go test ./...

# Integration tests (require PostgreSQL)
MITIGADOR_TEST_PG_DSN=postgres://localhost/mitigador_test go test \
  -tags=integration -count=1 -v ./test/integration/
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
