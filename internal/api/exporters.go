package api

import (
	"net/http"
	"time"

	"github.com/mitigador/mitigador/internal/ingest"
)

// handleListExporters handles GET /api/exporters.
// Returns {"items":[...]} from HealthTracker.Snapshot.
func handleListExporters(inv *ingest.Inventory, health *ingest.HealthTracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := health.Snapshot(inv, time.Now())
		items := make([]map[string]any, len(snap))
		for i, e := range snap {
			var lastSeen any
			if !e.LastSeen.IsZero() {
				lastSeen = e.LastSeen
			}
			items[i] = map[string]any{
				"source_ip":            e.SourceIP.String(),
				"type":                 e.Type,
				"last_seen":            lastSeen,
				"flows_per_sec":        e.FlowsPerSec,
				"status":               string(e.Status),
				"sample_rate_override": e.SampleRateOverride,
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
