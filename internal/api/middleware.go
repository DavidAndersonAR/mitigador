package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5/middleware"
)

// requireAuth returns a middleware that checks for an authenticated session.
// Returns 401 JSON if sm.GetInt64(ctx, "user_id") == 0.
func requireAuth(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid := sm.GetInt64(r.Context(), "user_id")
			if uid == 0 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not_authenticated"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// slogMiddleware logs method, path, status, and duration via slog.
func slogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}
