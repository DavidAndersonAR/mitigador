package api

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mitigador/mitigador/internal/aggregate"
	"github.com/mitigador/mitigador/internal/detect"
	"github.com/mitigador/mitigador/internal/incident"
	"github.com/mitigador/mitigador/internal/ingest"
	"github.com/mitigador/mitigador/internal/user"
)

// Deps are the runtime dependencies the API server needs.
type Deps struct {
	Pool      *pgxpool.Pool
	SM        *scs.SessionManager
	Users     *user.Store
	Incidents *incident.Store
	Inventory *ingest.Inventory
	Health    *ingest.HealthTracker
	SSEBroker *Broker
	Store     *aggregate.Store  // per-host counter source for /api/traffic/*
	Catalog   *detect.Catalog   // longest-prefix-match for hostgroup labels
}

// New returns an http.Handler with all routes mounted.
//
// Route layout:
//
//	Public (no auth):
//	  POST /api/auth/login
//	  GET  /api/auth/csrf
//
//	Authenticated + CSRF-checked (non-GET):
//	  POST /api/auth/logout
//	  GET  /api/auth/me
//	  GET  /api/incidents
//	  GET  /api/incidents/{id}
//	  GET  /api/exporters
//	  GET  /api/bgp/sessions
//	  GET  /api/traffic/top20
//	  GET  /api/traffic/host/{ip}
//	  GET  /api/events  (SSE)
//
//	Static SPA catch-all (LAST, after all /api/* routes):
//	  /*
//
// Security note (T-01-10-09): the SPA catch-all is registered after /api/*
// routes. Any future authenticated endpoint not under /api/ must be registered
// before the staticHandler call.
func New(deps Deps) http.Handler {
	r := chi.NewRouter()

	// Global middleware stack.
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(slogMiddleware)
	r.Use(deps.SM.LoadAndSave)

	// Public endpoints — no auth, no CSRF.
	r.Post("/api/auth/login", handleLogin(deps.Users, deps.SM))
	r.Get("/api/auth/csrf", handleCSRF(deps.SM))

	// Authenticated group — requires session; non-GET requests also need CSRF.
	r.Group(func(p chi.Router) {
		p.Use(requireAuth(deps.SM))
		p.Use(csrfMiddleware(deps.SM))

		p.Post("/api/auth/logout", handleLogout(deps.SM))
		p.Get("/api/auth/me", handleMe(deps.Users, deps.SM))
		p.Get("/api/incidents", handleListIncidents(deps.Incidents))
		p.Get("/api/incidents/{id}", handleGetIncident(deps.Incidents))
		p.Get("/api/exporters", handleListExporters(deps.Inventory, deps.Health))
		p.Get("/api/bgp/sessions", handleBGPStub())
		p.Get("/api/traffic/top20", handleTrafficTop20(deps))
		p.Get("/api/traffic/host/{ip}", handleTrafficHost(deps))
		p.Get("/api/events", deps.SSEBroker.Handler)
	})

	// SPA static catch-all — must come LAST so /api/* routes take priority.
	r.Handle("/*", staticHandler())

	return r
}
