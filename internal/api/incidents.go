package api

import (
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mitigador/mitigador/internal/detect"
	"github.com/mitigador/mitigador/internal/incident"
)

// handleListIncidents handles GET /api/incidents with optional query filters.
// Returns {"items":[...],"total":N}.
func handleListIncidents(store *incident.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := incident.Filter{}

		if v := q.Get("vector"); v != "" {
			if v != string(detect.VectorUDPFlood) && v != string(detect.VectorICMPFlood) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param", "param": "vector"})
				return
			}
			vec := detect.Vector(v)
			f.Vector = &vec
		}

		if v := q.Get("host_ip"); v != "" {
			addr, err := netip.ParseAddr(v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param", "param": "host_ip"})
				return
			}
			f.HostIP = &addr
		}

		if v := q.Get("since"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param", "param": "since"})
				return
			}
			f.Since = &t
		}

		if v := q.Get("until"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param", "param": "until"})
				return
			}
			f.Until = &t
		}

		if q.Get("active") == "true" {
			f.ActiveOnly = true
		}

		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param", "param": "limit"})
				return
			}
			f.Limit = n
		}

		if v := q.Get("offset"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_param", "param": "offset"})
				return
			}
			f.Offset = n
		}

		result, err := store.List(r.Context(), f)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}

		items := make([]map[string]any, len(result.Items))
		for i, inc := range result.Items {
			items[i] = map[string]any{
				"id":         inc.ID,
				"host_ip":    inc.HostIP.String(),
				"vector":     string(inc.Vector),
				"hostgroup":  inc.Hostgroup,
				"started_at": inc.StartedAt,
				"ended_at":   inc.EndedAt,
				"peak_pps":   inc.PeakPps,
				"peak_bps":   inc.PeakBps,
				"score":      inc.Score,
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": result.Total})
	}
}

// handleGetIncident handles GET /api/incidents/:id.
// Returns {"incident":{...},"updates":[...]} or 404.
func handleGetIncident(store *incident.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		inc, updates, err := store.Get(r.Context(), id)
		if errors.Is(err, incident.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"incident": map[string]any{
				"id":         inc.ID,
				"host_ip":    inc.HostIP.String(),
				"vector":     string(inc.Vector),
				"hostgroup":  inc.Hostgroup,
				"started_at": inc.StartedAt,
				"ended_at":   inc.EndedAt,
				"peak_pps":   inc.PeakPps,
				"peak_bps":   inc.PeakBps,
				"score":      inc.Score,
			},
			"updates": updates,
		})
	}
}

// handleBGPStub handles GET /api/bgp/sessions.
// Returns {"items":[]} — D-18 stub (GoBGP not wired in Phase 1).
func handleBGPStub() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
	}
}
