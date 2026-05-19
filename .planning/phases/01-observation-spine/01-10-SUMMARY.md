---
phase: "01"
plan: "10"
subsystem: api
tags: [http-api, chi, scs, session, csrf, sse, embed-fs, auth, dash-01, dash-02, dash-04, dash-05]
dependency_graph:
  requires: [01-03-config, 01-04-user-store, 01-08-incident-store, 01-09-alert-bus]
  provides: [http-api-server, session-manager, sse-broker, spa-static-handler, dash-01, dash-02, dash-04, dash-05]
  affects: [01-11-dashboard, 01-12-serve-wiring]
tech_stack:
  added:
    - "github.com/go-chi/chi/v5 v5.2.5 — HTTP router (stdlib net/http compatible)"
    - "github.com/alexedwards/scs/v2 v2.9.0 — server-side session manager"
    - "github.com/alexedwards/scs/pgxstore v0.0.0-20251002162104-209de6e426de — Postgres-backed session store"
  patterns:
    - "scs/v2 LoadAndSave middleware on chi router — session cookie set on every response"
    - "requireAuth middleware reads sm.GetInt64(ctx,'user_id'); returns 401 JSON if 0"
    - "csrfMiddleware: random 32-byte hex token stored in session; X-CSRF-Token header echoed on non-GET"
    - "SSE Broker: subscribe/unsubscribe via buffered chan of chan ssePayload; drop-on-full per client"
    - "embed.FS with //go:embed all:web_dist; SPA fallback to index.html for unknown paths"
    - "writeJSON helper: always sets Content-Type before WriteHeader; no internal errors in body (T-01-10-10)"
key_files:
  created:
    - internal/session/manager.go
    - internal/api/server.go
    - internal/api/auth.go
    - internal/api/auth_test.go
    - internal/api/csrf.go
    - internal/api/csrf_test.go
    - internal/api/middleware.go
    - internal/api/sse.go
    - internal/api/sse_test.go
    - internal/api/incidents.go
    - internal/api/incidents_test.go
    - internal/api/exporters.go
    - internal/api/exporters_test.go
    - internal/api/static.go
    - internal/api/web_dist/index.html
  modified:
    - go.mod
    - go.sum
decisions:
  - "CSRF middleware in authenticated group only — login is public (no token needed to authenticate); GET /api/auth/csrf issues token without requiring prior auth so the SPA can fetch it pre-login if needed"
  - "SSE heartbeat as SSE comment (': heartbeat') every 15s — keeps proxies from closing idle connections; zero-value ssePayload signals heartbeat vs real event in Handler select"
  - "SPA static handler registered LAST via r.Handle('/*') — chi routes /api/* first, so the catch-all never shadows API routes (T-01-10-09)"
  - "Inventory passed as *ingest.Inventory to handleListExporters; plan 12 wires a real pool-backed Inventory; tests use zero-value struct (Snapshot returns empty slice)"
metrics:
  duration_seconds: 1200
  completed_date: "2026-05-19"
  tasks_completed: 3
  files_created: 15
  files_modified: 2
---

# Phase 1 Plan 10: HTTP API + SSE Summary

**One-liner:** chi v5 HTTP server with scs/v2 session-based auth (DASH-01), SSE broker for live attacks (DASH-02), paginated incident/exporter read APIs (PERS-01/DASH-05), BGP stub (DASH-04/D-18), CSRF middleware, and embed.FS SPA handler.

## What Was Built

### API Contract

All routes under `/api/`:

| Method | Path | Auth | CSRF | Description |
|--------|------|------|------|-------------|
| POST | `/api/auth/login` | No | No | Login; sets `mitigador_session` cookie |
| POST | `/api/auth/logout` | Yes | Yes | Destroys session |
| GET | `/api/auth/csrf` | No | — | Returns per-session CSRF token |
| GET | `/api/auth/me` | Yes | — | Returns `{id, username, email, last_login}` |
| GET | `/api/incidents` | Yes | — | Paginated incident list with filters |
| GET | `/api/incidents/{id}` | Yes | — | Single incident + updates; 404 if missing |
| GET | `/api/exporters` | Yes | — | Exporter health snapshot |
| GET | `/api/bgp/sessions` | Yes | — | `{"items":[]}` stub (D-18) |
| GET | `/api/events` | Yes | — | SSE stream of attack events + heartbeats |
| GET | `/*` | — | — | SPA static files (embed.FS) with fallback |

### session.NewManager defaults

| Field | Value |
|-------|-------|
| Store | pgxstore (Postgres sessions table) |
| Lifetime | 12h |
| IdleTimeout | 1h |
| Cookie.Name | `mitigador_session` |
| Cookie.HttpOnly | `true` |
| Cookie.Secure | `true` |
| Cookie.SameSite | `Lax` |
| Cookie.Path | `/` |

### Auth Security Properties

- **Session fixation defense (T-01-10-02):** `sm.RenewToken(ctx)` is called BEFORE `sm.Put(ctx, "user_id", u.ID)` in `handleLogin`. Confirmed by test `TestLogin_RenewsToken_BeforePuttingUserID`.
- **User enumeration defense (T-01-10-04):** Both "user not found" and "wrong password" return identical body `{"error":"invalid_credentials"}`. Confirmed by tests `TestLogin_WrongPassword_Returns401WithUniformError` and `TestLogin_NonexistentUser_Returns401WithUniformError`.
- **Error body hygiene (T-01-10-10):** All error responses use small JSON envelopes; no `err.Error()` text appears in response bodies. `writeJSON` is the only path to write JSON responses.

### CSRF Implementation

- `GET /api/auth/csrf` → creates a 32-byte random hex token, stores in session under key `"csrf_token"`, returns `{"token":"..."}`.
- Non-GET requests in the authenticated group must send `X-CSRF-Token: <token>` matching the session value; mismatch → 403 `{"error":"csrf_invalid"}`.
- SameSite=Lax on the session cookie provides defense-in-depth for top-level navigation.

### SSE Event Types

| SSE event field | Value |
|-----------------|-------|
| `event:` | `attack.started` \| `attack.update` \| `attack.ended` |
| `id:` | incident ULID |
| `data:` | JSON of AttackEvent fields |
| Heartbeat | `: heartbeat` (SSE comment, every 15s) |

Heartbeat cadence: `time.NewTicker(15 * time.Second)` in `Broker.Run`.

Per-client buffer: 16 events. Full buffer → drop silently (T-01-10-07). Client disconnect → `r.Context().Done()` returns in Handler, `unsubscribe` sent to broker, client channel closed.

### Static Handler Embed Path

`//go:embed all:web_dist` in `internal/api/static.go` embeds everything under `internal/api/web_dist/`.

- Plan 11 must write the Vue build output into `internal/api/web_dist/` (or symlink).
- `internal/api/web_dist/index.html` is a placeholder that renders "Mitigador — SPA not built."
- Unknown paths fall back to `index.html` (SPA routing via vue-router).

### Query Parameters for /api/incidents

| Param | Type | Validation |
|-------|------|-----------|
| `vector` | `udp_flood` \| `icmp_flood` | 400 with `param=vector` if invalid |
| `host_ip` | netip.Addr | 400 with `param=host_ip` if unparseable |
| `since` | RFC3339 | 400 with `param=since` if unparseable |
| `until` | RFC3339 | 400 with `param=until` if unparseable |
| `active` | `true` | sets ActiveOnly on Filter |
| `limit` | int 1–500 | 400 with `param=limit` if < 1 or non-integer |
| `offset` | int ≥ 0 | 400 with `param=offset` if < 0 or non-integer |

## Notes for Plan 11 (Dashboard Frontend)

1. **CSRF:** After login, call `GET /api/auth/csrf` once and store the token. Echo it as `X-CSRF-Token` header on all non-GET requests. In Phase 1, the only non-GET after login is `POST /api/auth/logout`.
2. **SSE reconnect:** Use `EventSource` with `Last-Event-ID` support (`@microsoft/fetch-event-source` recommended per UI-SPEC). On reconnect, re-fetch `/api/incidents?active=true` to sync live attack table state.
3. **No `v-html` on user-supplied data:** The API serializes incident data as JSON; Vue's default text interpolation auto-escapes. Do not use `v-html` on any field that originates from flow telemetry or incident details (T-01-10-06).

## Notes for Plan 12 (Serve Wiring)

1. **TLS termination:** `Cookie.Secure=true` requires HTTPS — browsers will not send the cookie over HTTP. Plan 12 must document the reverse-proxy TLS setup (nginx/caddy in front of the Go binary).
2. **SSE subscription:** Call `bus.Subscribe("sse")` BEFORE `bus.Run(ctx)` to get the SSE channel. Pass it to `api.NewBroker(ch)`. Start `broker.Run(ctx)` in a goroutine.
3. **Inventory:** Pass a real `*ingest.Inventory` (loaded from DB) and `*ingest.HealthTracker` to `api.Deps`. The zero-value Inventory used in tests returns an empty Snapshot.
4. **Rate limiting on login (T-01-10-01):** bcrypt cost 12 provides ~300ms per check as passive throttle. A per-IP `golang.org/x/time/rate` limiter on `/api/auth/login` is recommended as a follow-up in plan 12 or a dedicated plan.

## Task Commits

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | session.NewManager + auth/csrf/middleware | `6e478ab` | session/manager.go, api/auth.go, api/csrf.go, api/middleware.go, api/server.go, tests |
| 2 | Read endpoints — incidents, exporters, BGP stub | `d2fdda2` | api/incidents.go, api/exporters.go, tests |
| 3 | SSE Broker + static SPA handler | `588aaef` | api/sse.go, api/static.go, api/web_dist/index.html, tests |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TestHandler_HeadersSet: nil response from streaming HTTP client**
- **Found during:** Task 3 SSE test execution
- **Issue:** The test used `http.DefaultClient.Do(req)` with a 200ms context timeout against a real httptest.Server. The SSE handler holds the connection open, so the client call returned `context.DeadlineExceeded` with `resp == nil` before headers could be read.
- **Fix:** Rewrote the test to call `broker.Handler(w, req)` directly using a `flusherWriter` (pipe-backed ResponseWriter + Flusher). Headers are set synchronously in the handler before the event select loop, so they are readable immediately after a 20ms sleep. Test then cancels the request context to unblock the handler goroutine.
- **Files modified:** `internal/api/sse_test.go`
- **Committed in:** `588aaef` (Task 3)

**2. [Rule 1 - Bug] setupAuthClient returned 4 values in exporters_test.go but function signature returns 3**
- **Found during:** Task 2 — initial exporters_test.go used `srvURL, client, _, _ := setupAuthClient(t)` but `setupAuthClient` was later defined with 3 return values
- **Fix:** Updated all call sites in exporters_test.go to use 3-value destructuring `srvURL, client, _ := setupAuthClient(t)`
- **Files modified:** `internal/api/exporters_test.go`
- **Committed in:** `d2fdda2` (Task 2)

## Known Stubs

- `internal/api/web_dist/index.html` — placeholder "SPA not built" page. Plan 11 overwrites with Vue build output.
- `GET /api/bgp/sessions` — always returns `{"items":[]}`. Wired to real GoBGP in Phase 2.

The stubs do not prevent plan goals from being achieved: DASH-04 explicitly specifies an empty stub (D-18), and the SPA placeholder is intentional pending plan 11.

## Threat Flags

No new threat surface beyond the plan's threat model. All endpoints are under `/api/` (documented in threat model). The embed.FS catch-all `/*` is registered last in chi — existing `/api/*` routes take priority (T-01-10-09 satisfied).

## Self-Check: PASSED

Files created:
- FOUND: internal/session/manager.go
- FOUND: internal/api/server.go
- FOUND: internal/api/auth.go
- FOUND: internal/api/auth_test.go
- FOUND: internal/api/csrf.go
- FOUND: internal/api/csrf_test.go
- FOUND: internal/api/middleware.go
- FOUND: internal/api/sse.go
- FOUND: internal/api/sse_test.go
- FOUND: internal/api/incidents.go
- FOUND: internal/api/incidents_test.go
- FOUND: internal/api/exporters.go
- FOUND: internal/api/exporters_test.go
- FOUND: internal/api/static.go
- FOUND: internal/api/web_dist/index.html

Commits:
- FOUND: 6e478ab — feat(01-10): session.NewManager + chi server + auth/csrf handlers (Task 1)
- FOUND: d2fdda2 — feat(01-10): read endpoints — /api/incidents, /api/exporters, /api/bgp/sessions stub (Task 2)
- FOUND: 588aaef — feat(01-10): SSE Broker + /api/events + static SPA handler (Task 3)

Verifications:
- `go build ./...` exits 0
- `go vet ./internal/api/... ./internal/session/...` exits 0
- `go test ./internal/api/... -count=1` → 4 SSE tests PASS; 19 DB-gated tests SKIP without DSN
- DASH-01: session-based login with HttpOnly/Secure/SameSite=Lax cookie
- DASH-02: SSE streams attack.started/update/ended + 15s heartbeat
- DASH-04: /api/bgp/sessions returns {"items":[]} (D-18)
- DASH-05: /api/exporters returns health snapshot with source_ip/type/last_seen/flows_per_sec/status/sample_rate_override
- CSRF: X-CSRF-Token required on non-GET in authenticated group; 403 on mismatch
- SPA: embed.FS with SPA fallback; placeholder index.html committed
- Unauthenticated /api/* returns 401 (except login and csrf)
