package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mitigador/mitigador/internal/detect"
)

type ssePayload struct {
	ID   string
	Type string
	Data []byte
}

// Broker fans attack events from the alert.Bus SSE subscription to N HTTP clients.
type Broker struct {
	in          <-chan detect.AttackEvent
	subscribe   chan chan ssePayload
	unsubscribe chan chan ssePayload
}

// NewBroker takes the channel that alert.Bus.Subscribe gave the SSE sink.
func NewBroker(in <-chan detect.AttackEvent) *Broker {
	return &Broker{
		in:          in,
		subscribe:   make(chan chan ssePayload, 4),
		unsubscribe: make(chan chan ssePayload, 4),
	}
}

// Run blocks until ctx is cancelled. It fans events to all connected clients.
// Each client has a 16-event buffer; full buffers receive drop-on-full (T-01-10-07).
// Heartbeats are sent every 15s as SSE comments (": heartbeat\n\n").
func (b *Broker) Run(ctx context.Context) error {
	clients := map[chan ssePayload]struct{}{}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			for c := range clients {
				close(c)
			}
			return ctx.Err()

		case c := <-b.subscribe:
			clients[c] = struct{}{}

		case c := <-b.unsubscribe:
			delete(clients, c)
			close(c)

		case ev, ok := <-b.in:
			if !ok {
				for c := range clients {
					close(c)
				}
				return nil
			}
			// Map State → SSE event type.
			evType := "attack." + string(ev.State)
			payload, err := json.Marshal(eventToMap(ev))
			if err != nil {
				slog.Error("sse: marshal event", "err", err.Error())
				continue
			}
			p := ssePayload{ID: ev.IncidentID, Type: evType, Data: payload}
			for c := range clients {
				select {
				case c <- p:
				default:
					// Drop silently — slow client (T-01-10-07).
				}
			}

		case <-tick.C:
			// zero-value payload signals heartbeat to Handler.
			hb := ssePayload{}
			for c := range clients {
				select {
				case c <- hb:
				default:
				}
			}
		}
	}
}

// Handler is the authenticated HTTP handler for GET /api/events (SSE).
// It sets all required headers and streams events until the client disconnects.
func (b *Broker) Handler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan ssePayload, 16)
	b.subscribe <- ch
	defer func() {
		b.unsubscribe <- ch
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case p, ok := <-ch:
			if !ok {
				return
			}
			if p.ID == "" && p.Type == "" {
				// heartbeat comment
				fmt.Fprint(w, ": heartbeat\n\n")
			} else {
				fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", p.ID, p.Type, p.Data)
			}
			flusher.Flush()
		}
	}
}

// eventToMap converts an AttackEvent to a JSON-serialisable map.
func eventToMap(ev detect.AttackEvent) map[string]any {
	return map[string]any{
		"incident_id": ev.IncidentID,
		"state":       string(ev.State),
		"host_ip":     ev.HostIP.String(),
		"vector":      string(ev.Vector),
		"hostgroup":   ev.Hostgroup,
		"pps":         ev.Pps,
		"bps":         ev.Bps,
		"peak_pps":    ev.PeakPps,
		"peak_bps":    ev.PeakBps,
		"confidence":  ev.Confidence,
		"started_at":  ev.StartedAt,
		"ended_at":    ev.EndedAt,
		"now":         ev.Now,
	}
}
