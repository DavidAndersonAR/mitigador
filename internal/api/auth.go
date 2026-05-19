package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/mitigador/mitigador/internal/user"
)

// handleLogin handles POST /api/auth/login.
// On success: RenewToken, Put user_id, UpdateLastLogin, return 204.
// On wrong creds: 401 {"error":"invalid_credentials"}.
// On bad JSON: 400.
func handleLogin(users *user.Store, sm *scs.SessionManager) http.HandlerFunc {
	type req struct {
		Username string
		Password string
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var body req
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
			return
		}
		u, err := users.VerifyPassword(r.Context(), body.Username, body.Password)
		if err != nil {
			// Uniform response regardless of whether user doesn't exist or password is wrong.
			// Prevents user enumeration (T-01-10-04).
			if errors.Is(err, user.ErrNotFound) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
			return
		}
		// Session fixation defense (T-01-10-02): RenewToken BEFORE putting user_id.
		if err := sm.RenewToken(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session_error"})
			return
		}
		sm.Put(r.Context(), "user_id", u.ID)
		_ = users.UpdateLastLogin(r.Context(), u.ID)
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleLogout handles POST /api/auth/logout.
// Destroys the session and returns 204.
func handleLogout(sm *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = sm.Destroy(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleMe handles GET /api/auth/me.
// Returns the logged-in user's fields or 401 if unauthenticated.
func handleMe(users *user.Store, sm *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := sm.GetInt64(r.Context(), "user_id")
		if uid == 0 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not_authenticated"})
			return
		}
		u, err := users.GetByID(r.Context(), uid)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not_authenticated"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":         u.ID,
			"username":   u.Username,
			"email":      u.Email,
			"last_login": u.LastLogin,
		})
	}
}

// writeJSON writes a JSON body with the given status code.
// No internal error details leak through (T-01-10-10).
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
