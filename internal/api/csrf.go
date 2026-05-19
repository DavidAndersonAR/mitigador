package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/alexedwards/scs/v2"
)

const csrfSessionKey = "csrf_token"

// csrfTokenFromSession returns the per-session CSRF token, creating a new
// random 32-byte hex token if one is absent.
func csrfTokenFromSession(sm *scs.SessionManager, r *http.Request) (string, error) {
	tok := sm.GetString(r.Context(), csrfSessionKey)
	if tok != "" {
		return tok, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok = hex.EncodeToString(raw)
	sm.Put(r.Context(), csrfSessionKey, tok)
	return tok, nil
}

// csrfMiddleware checks X-CSRF-Token header against the session value for
// non-GET, non-HEAD, non-OPTIONS requests. Returns 403 on mismatch.
//
// Note: login, logout are registered so that login is public (no CSRF needed
// for initial login), and the CSRF check is applied to the authenticated group
// which includes logout. GET /api/auth/csrf is excluded by the GET method.
func csrfMiddleware(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Safe methods are exempt.
			if r.Method == http.MethodGet ||
				r.Method == http.MethodHead ||
				r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			tok := sm.GetString(r.Context(), csrfSessionKey)
			if tok == "" || r.Header.Get("X-CSRF-Token") != tok {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "csrf_invalid"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// handleCSRF handles GET /api/auth/csrf — returns the per-session token.
func handleCSRF(sm *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, err := csrfTokenFromSession(sm, r)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"token": tok})
	}
}
