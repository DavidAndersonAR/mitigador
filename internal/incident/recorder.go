package incident

import (
	"context"
	"log/slog"
	"time"

	"github.com/mitigador/mitigador/internal/detect"
)

// Recorder consumes AttackEvents from a channel and persists them via Store.
// It is resilient to transient DB errors: errors are logged but never halt the loop.
type Recorder struct {
	store *Store
	in    <-chan detect.AttackEvent
}

// NewRecorder creates a Recorder that reads from in and writes via store.
func NewRecorder(store *Store, in <-chan detect.AttackEvent) *Recorder {
	return &Recorder{store: store, in: in}
}

// Run consumes events from the input channel until ctx is canceled.
// On cancellation it drains any remaining buffered events within a 2-second window.
// Always returns ctx.Err().
func (r *Recorder) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			// Drain remaining buffered events with a short timeout.
			drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			r.drain(drainCtx)
			cancel()
			return ctx.Err()
		case ev := <-r.in:
			r.handle(ctx, ev)
		}
	}
}

// drain reads from the channel until it is empty or drainCtx expires.
func (r *Recorder) drain(drainCtx context.Context) {
	for {
		select {
		case <-drainCtx.Done():
			return
		case ev, ok := <-r.in:
			if !ok {
				return
			}
			r.handle(drainCtx, ev)
		default:
			// Nothing buffered — drain complete.
			return
		}
	}
}

// handle dispatches a single AttackEvent to the appropriate Store method.
// DB errors are logged but do not halt the loop.
func (r *Recorder) handle(ctx context.Context, ev detect.AttackEvent) {
	var err error
	switch ev.State {
	case detect.StateStarted:
		err = r.store.Create(ctx, ev)
	case detect.StateUpdated:
		err = r.store.Update(ctx, ev)
	case detect.StateEnded:
		err = r.store.End(ctx, ev)
	default:
		slog.Warn("incident.recorder: unknown state",
			"state", string(ev.State),
			"incident_id", ev.IncidentID,
		)
		return
	}
	if err != nil {
		slog.Error("incident.recorder: persist failed",
			"incident_id", ev.IncidentID,
			"state", string(ev.State),
			"host_ip", ev.HostIP.String(),
			"vector", string(ev.Vector),
			"err", err.Error(),
		)
	}
}
