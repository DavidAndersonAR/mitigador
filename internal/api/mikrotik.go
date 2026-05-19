package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mitigador/mitigador/internal/subscriber"
)

// dashRouterDTO is the public-facing shape — never includes the raw password.
type dashRouterDTO struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Username   string    `json:"username"`
	HasPassword bool     `json:"has_password"` // true if a password is set; the value itself stays server-side
	VerifyTLS  bool      `json:"verify_tls"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toDTO(r subscriber.Router) dashRouterDTO {
	return dashRouterDTO{
		ID:          r.ID,
		Name:        r.Name,
		URL:         r.URL,
		Username:    r.Username,
		HasPassword: r.Password != "",
		VerifyTLS:   r.VerifyTLS,
		Enabled:     r.Enabled,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// handleListMikrotikRouters handles GET /api/mikrotik/routers.
func handleListMikrotikRouters(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.MikrotikStore == nil {
			writeJSON(w, http.StatusOK, map[string]any{"items": []dashRouterDTO{}})
			return
		}
		routers, err := deps.MikrotikStore.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list_failed"})
			return
		}
		out := make([]dashRouterDTO, len(routers))
		for i, rt := range routers {
			out[i] = toDTO(rt)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": out})
	}
}

type createRouterReq struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	VerifyTLS bool   `json:"verify_tls"`
	Enabled   *bool  `json:"enabled"` // pointer so omitted = default true
}

func (c *createRouterReq) validate() string {
	if strings.TrimSpace(c.Name) == "" {
		return "name is required"
	}
	if strings.TrimSpace(c.URL) == "" {
		return "url is required"
	}
	if !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") {
		return "url must start with http:// or https://"
	}
	if strings.TrimSpace(c.Username) == "" {
		return "username is required"
	}
	if c.Password == "" {
		return "password is required"
	}
	return ""
}

// handleCreateMikrotikRouter handles POST /api/mikrotik/routers.
func handleCreateMikrotikRouter(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.MikrotikStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable"})
			return
		}
		var req createRouterReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		if msg := req.validate(); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param", "detail": msg})
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		created, err := deps.MikrotikStore.Create(r.Context(), subscriber.Router{
			Name:      strings.TrimSpace(req.Name),
			URL:       strings.TrimRight(strings.TrimSpace(req.URL), "/"),
			Username:  strings.TrimSpace(req.Username),
			Password:  req.Password,
			VerifyTLS: req.VerifyTLS,
			Enabled:   enabled,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "name_taken"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create_failed"})
			return
		}
		writeJSON(w, http.StatusCreated, toDTO(created))
	}
}

type updateRouterReq struct {
	Name      *string `json:"name"`
	URL       *string `json:"url"`
	Username  *string `json:"username"`
	Password  *string `json:"password"` // omit field to keep existing password
	VerifyTLS *bool   `json:"verify_tls"`
	Enabled   *bool   `json:"enabled"`
}

// handlePatchMikrotikRouter handles PATCH /api/mikrotik/routers/{id}.
func handlePatchMikrotikRouter(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.MikrotikStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable"})
			return
		}
		id, err := parseInt64(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
			return
		}
		var req updateRouterReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		updated, err := deps.MikrotikStore.Update(r.Context(), id, subscriber.UpdateFields{
			Name:      req.Name,
			URL:       req.URL,
			Username:  req.Username,
			Password:  req.Password,
			VerifyTLS: req.VerifyTLS,
			Enabled:   req.Enabled,
		})
		if errors.Is(err, subscriber.ErrRouterNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		if err != nil {
			if isUniqueViolation(err) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "name_taken"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "update_failed"})
			return
		}
		writeJSON(w, http.StatusOK, toDTO(updated))
	}
}

// handleDeleteMikrotikRouter handles DELETE /api/mikrotik/routers/{id}.
func handleDeleteMikrotikRouter(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.MikrotikStore == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable"})
			return
		}
		id, err := parseInt64(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
			return
		}
		if err := deps.MikrotikStore.Delete(r.Context(), id); err != nil {
			if errors.Is(err, subscriber.ErrRouterNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete_failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type testRouterReq struct {
	URL       string `json:"url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	VerifyTLS bool   `json:"verify_tls"`
}

// handleTestMikrotikRouter handles POST /api/mikrotik/routers/test.
// Body carries the credentials to try (without persisting them). The
// response includes the router's reported identity on success.
func handleTestMikrotikRouter() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req testRouterReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		if req.URL == "" || req.Username == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		name, err := subscriber.TestConnection(ctx, subscriber.RouterConfig{
			Name:      "probe",
			URL:       req.URL,
			Username:  req.Username,
			Password:  req.Password,
			VerifyTLS: req.VerifyTLS,
			Timeout:   5 * time.Second,
		})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"identity": name,
		})
	}
}

// ─── helpers ────────────────────────────────────────────────────────

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// isUniqueViolation matches Postgres' SQLSTATE 23505 (unique_violation)
// without depending on the pgx error type directly — keeps this file free
// of pgx imports.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "SQLSTATE 23505") || strings.Contains(s, "unique constraint")
}

// compile-time guard: keep fmt import for future error wrapping.
var _ = fmt.Sprintf
