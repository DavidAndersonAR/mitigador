---
phase: "01"
plan: "11"
subsystem: dashboard
tags: [vue3, vite, naive-ui, pinia, vue-router, vue-i18n, sse, spa, dash-01, dash-02, dash-04, dash-05, dash-09]
dependency_graph:
  requires: [01-10-http-api]
  provides: [spa-dashboard, login-view, live-attacks-view, exporters-view, bgp-stub-view, incident-list-view, incident-detail-view]
  affects: [01-12-serve-wiring]
tech_stack:
  added:
    - "vue@3.5.34 — Composition API <script setup>"
    - "naive-ui@2.44.1 — dark theme component library"
    - "vite@5.4.21 — build tool, outDir → internal/api/web_dist"
    - "vue-router@4.6.4 — SPA routing with auth guard"
    - "pinia@2.3.1 — auth + incidents stores"
    - "vue-i18n@9.14.5 — pt-BR/en-US locale toggle"
    - "@microsoft/fetch-event-source@2.0.1 — SSE with Last-Event-ID reconnect"
    - "@vicons/ionicons5@0.12.0 — icon set for BGP stub view"
    - "vue-tsc@2.2.12 — TypeScript strict mode checks"
  patterns:
    - "api() wrapper: fetch with credentials:include + X-CSRF-Token on non-GET"
    - "connectEvents(): @microsoft/fetch-event-source with onOpen/onMessage/onError callbacks"
    - "useIncidentsStore.sseStatus drives SSEIndicator dot color (green/amber/red)"
    - "attack.started → unshift + 1s flash; attack.ended → strikethrough + 10s fade then splice"
    - "router.beforeEach → auth.ensureLoaded() → redirect /login if no user"
    - "AppLayout provides header slot (title) + locale toggle + SSEIndicator in header-right"
    - "Vite emptyOutDir:true replaces placeholder index.html with full SPA on each build"
key_files:
  created:
    - web/package.json
    - web/pnpm-lock.yaml
    - web/vite.config.ts
    - web/tsconfig.json
    - web/index.html
    - web/.gitignore
    - web/src/main.ts
    - web/src/App.vue
    - web/src/style.css
    - web/src/router/index.ts
    - web/src/locales/index.ts
    - web/src/locales/pt-BR.json
    - web/src/locales/en-US.json
    - web/src/api/client.ts
    - web/src/api/sse.ts
    - web/src/stores/auth.ts
    - web/src/stores/incidents.ts
    - web/src/components/AppLayout.vue
    - web/src/components/SidebarNav.vue
    - web/src/components/SSEIndicator.vue
    - web/src/views/LoginView.vue
    - web/src/views/DashboardView.vue
    - web/src/views/ExportersView.vue
    - web/src/views/BGPStubView.vue
    - web/src/views/IncidentListView.vue
    - web/src/views/IncidentDetailView.vue
  modified:
    - internal/api/web_dist/index.html (replaced placeholder with Vite SPA output)
    - internal/api/web_dist/assets/ (Vite build artifacts)
decisions:
  - "Manual project scaffold (no pnpm create vite) — interactive CLI prompts cancelled in non-TTY shell; writing package.json directly gives identical result with full control"
  - "SidebarNav uses router-link with active-class instead of NMenu — avoids Naive UI menu key-matching complexity for simple 4-item nav; active green left-border matches UI-SPEC exactly"
  - "DashboardView manages per-row flash/fade with Set<string> refs + per-timer Map — avoids storing transient UI state in Pinia (only persistent attack data lives in the store)"
  - "Exporter warming detection: last_seen === null signals no flow templates received yet — simple, matches the API contract from plan 10"
metrics:
  duration_seconds: 2700
  completed_date: "2026-05-19"
  tasks_completed: 4
  files_created: 26
  files_modified: 2
---

# Phase 1 Plan 11: Vue 3 Dashboard Summary

**One-liner:** Vue 3 + Naive UI dark-theme SPA with SSE-connected live attacks dashboard, pt-BR/en-US i18n, 6 views (login, dashboard, exporters, BGP stub, incident list/detail), all wired to the plan-10 API via cookie-session auth and CSRF-safe fetch wrapper.

## What Was Built

### Locked Dependency Versions

| Package | Version |
|---------|---------|
| vue | 3.5.34 |
| naive-ui | 2.44.1 |
| vue-router | 4.6.4 |
| pinia | 2.3.1 |
| vue-i18n | 9.14.5 |
| @microsoft/fetch-event-source | 2.0.1 |
| vite | 5.4.21 |
| vue-tsc | 2.2.12 |

### Routes Registered

| Path | Name | Component | Auth |
|------|------|-----------|------|
| `/login` | login | LoginView | No |
| `/` | dashboard | DashboardView | Yes |
| `/exporters` | exporters | ExportersView | Yes |
| `/bgp` | bgp | BGPStubView | Yes |
| `/incidents` | incidents | IncidentListView | Yes |
| `/incidents/:id` | incident | IncidentDetailView | Yes |

Auth guard: `router.beforeEach` calls `auth.ensureLoaded()` → redirects to `/login` if `auth.user === null`.

### i18n Key Count

- `web/src/locales/pt-BR.json`: **63 keys** covering all UI-SPEC §Copywriting Contract strings verbatim
- `web/src/locales/en-US.json`: **63 keys** (matching key set, English values)
- Default locale: `pt-BR` (read from `localStorage['mitigador.locale']`, fallback `pt-BR`)
- Locale toggle: header button calls `setLocale()` which updates `i18n.global.locale.value` + persists to localStorage

All UI-SPEC §Copywriting Contract strings confirmed present:
- `login.submit` = "Entrar" / "Sign In"
- `bgp.vazio.titulo` = "Nenhuma sessão BGP configurada"
- `ataques.vazio.titulo` = "Nenhum ataque detectado"
- `exporters.status.online/stale/offline` = "Online" / "Sem dados recentes" / "Offline"
- `sse.reconectando` = "Reconectando…"
- `sse.desconectado` = "Conexão perdida. Tentando reconectar."

### SSE Event Handling Pattern

```
attack.started  → incidents.active.unshift(row)
                  → flash row background green for 1s (per-row Set<string>)
attack.update   → Object.assign(existing row, {pps, bps, peak_pps, peak_bps, confidence})
attack.ended    → incidents.active[idx].ended = true
                  → add to fadingRows Set (opacity:0.3, text-decoration:line-through)
                  → after 10s: remove from fadingRows + splice from active array
SSE disconnect  → incidents.sseStatus = 'reconnecting'
                  → @microsoft/fetch-event-source retries automatically
SSE reconnect   → incidents.sseStatus = 'open'
                  → table stays in last known state (no wipe)
```

Row severity color-coding (left border): red 3px (confidence ≥ 80), amber 3px (40–79), transparent (< 40).

### BGP Stub View (D-18)

`BGPStubView.vue` imports only `useI18n`, `NEmpty`, `NIcon`, `ServerOutline`, and `AppLayout`. It contains **zero `api(` calls** — confirmed by grep. Title and body rendered from `bgp.vazio.titulo` / `bgp.vazio.body` i18n keys.

### Build Command for Plan 12

```bash
cd web && pnpm install && pnpm build
# Then: go build ./...
# web_dist/ is populated by pnpm build before the Go embed.FS needs it
```

## Task Commits

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Scaffold Vue 3 + Vite + TS project | `ccf8895` | package.json, vite.config.ts, locales/, router/, stores/, api/ |
| 2 | AppLayout + SidebarNav + SSEIndicator | `291762d` | components/AppLayout.vue, SidebarNav.vue, SSEIndicator.vue |
| 3a | LoginView + DashboardView (SSE-connected) | `1fdd019` | views/LoginView.vue, DashboardView.vue |
| 3b | ExportersView + BGPStubView + IncidentList + IncidentDetail | `920eb8d` | views/Exporters*.vue, BGP*.vue, Incident*.vue |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] pnpm create vite interactive prompt cancels in non-TTY shell**
- **Found during:** Task 1 scaffolding
- **Issue:** `pnpm create vite@latest . --template vue-ts` cancelled immediately in the non-interactive shell environment; stdin EOF triggered "Operation cancelled".
- **Fix:** Created all scaffold files (package.json, tsconfig.json, vite.config.ts, index.html, style.css) directly with Write tool — equivalent result with full control over content. All file contents match what `pnpm create vite --template vue-ts` would produce.
- **Files modified:** All web/ scaffold files created directly
- **Commit:** `ccf8895` (Task 1)

**2. [Rule 1 - Bug] Unused NMenu import + handleSelect function in SidebarNav.vue**
- **Found during:** Task 2 build check
- **Issue:** TypeScript strict mode (`noUnusedLocals`) rejected `NMenu` import and `handleSelect` function that were in the plan's draft code but not used in the final router-link-based nav implementation.
- **Fix:** Removed `NMenu` from import and dropped `handleSelect` (routing handled by `<router-link :to="...">` directly).
- **Files modified:** `web/src/components/SidebarNav.vue`
- **Commit:** `291762d` (Task 2)

## Known Stubs

None — all 6 views are fully implemented. BGPStubView is intentionally a permanent stub per D-18 (not a code stub — it is the complete Phase 1 implementation of that view).

## Threat Flags

No new threat surface beyond the plan's threat model.

- T-01-11-01 (XSS via v-html): `grep -rn "v-html" web/src/` returns 0 hits — all data rendered via `{{ }}` text interpolation. MITIGATED.
- T-01-11-03 (CSRF): `api()` wrapper attaches `X-CSRF-Token` on all non-GET requests. Login is public (no CSRF token needed). MITIGATED.
- T-01-11-06 (SSE memory leak): `@microsoft/fetch-event-source` has built-in exponential backoff; `onUnmounted` calls `ctrl.abort()` to clean up; per-row timers cleared in `onUnmounted`. MITIGATED.
- T-01-11-07 (localStorage locale injection): `i18n.fallbackLocale = 'pt-BR'` — unknown locale keys fall back gracefully. MITIGATED.

## Self-Check: PASSED

Files created:
- FOUND: web/package.json
- FOUND: web/pnpm-lock.yaml
- FOUND: web/vite.config.ts
- FOUND: web/tsconfig.json
- FOUND: web/index.html
- FOUND: web/.gitignore
- FOUND: web/src/main.ts
- FOUND: web/src/App.vue
- FOUND: web/src/style.css
- FOUND: web/src/router/index.ts
- FOUND: web/src/locales/index.ts
- FOUND: web/src/locales/pt-BR.json
- FOUND: web/src/locales/en-US.json
- FOUND: web/src/api/client.ts
- FOUND: web/src/api/sse.ts
- FOUND: web/src/stores/auth.ts
- FOUND: web/src/stores/incidents.ts
- FOUND: web/src/components/AppLayout.vue
- FOUND: web/src/components/SidebarNav.vue
- FOUND: web/src/components/SSEIndicator.vue
- FOUND: web/src/views/LoginView.vue
- FOUND: web/src/views/DashboardView.vue
- FOUND: web/src/views/ExportersView.vue
- FOUND: web/src/views/BGPStubView.vue
- FOUND: web/src/views/IncidentListView.vue
- FOUND: web/src/views/IncidentDetailView.vue
- FOUND: internal/api/web_dist/index.html

Commits:
- FOUND: ccf8895 — feat(01-11): scaffold Vue 3 + Vite + TS project (Task 1)
- FOUND: 291762d — feat(01-11): AppLayout + SidebarNav + SSEIndicator (Task 2)
- FOUND: 1fdd019 — feat(01-11): LoginView + DashboardView with SSE (Task 3a)
- FOUND: 920eb8d — feat(01-11): ExportersView + BGPStubView + IncidentList + IncidentDetail (Task 3b)

Verifications:
- pnpm install --frozen-lockfile exits 0
- pnpm build exits 0 (vue-tsc + vite build)
- internal/api/web_dist/index.html exists post-build
- go build ./... exits 0 (Go embed.FS picks up web_dist)
- DASH-01: Login view → POST /api/auth/login → redirect /
- DASH-02: DashboardView subscribes EventSource /api/events, handles attack.started/update/ended
- DASH-04: BGPStubView permanent stub (D-18), zero data fetch
- DASH-05: ExportersView fetches /api/exporters every 15s with status dots
- DASH-09: pt-BR default, EN toggle via vue-i18n + localStorage persistence
- No v-html anywhere (T-01-11-01)
- CSRF X-CSRF-Token header on all non-GET (T-01-11-03)
