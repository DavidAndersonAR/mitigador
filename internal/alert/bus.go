// Package alert fans out AttackEvent to Telegram and SMTP sinks.
//
// Filled in plan 01-09; see .planning/phases/01-observation-spine/01-09-PLAN.md.
package alert

import (
	"context"
	"log/slog"
	"sync"

	"github.com/mitigador/mitigador/internal/detect"
)

// Sink is anything that consumes AttackEvents from the bus.
// Each Sink runs in its own goroutine; the bus does not block on a slow sink.
type Sink interface {
	Name() string
	Run(ctx context.Context, in <-chan detect.AttackEvent) error
}

type subscriber struct {
	name string
	ch   chan detect.AttackEvent
}

// Bus fans out events from one input channel to N subscribers.
// Subscribe must be called before Run. Slow subscribers receive drop-on-full
// semantics — the bus never blocks waiting for a subscriber to drain.
type Bus struct {
	in            <-chan detect.AttackEvent
	perSinkBuffer int
	mu            sync.Mutex
	subs          []subscriber
}

// NewBus creates a Bus that reads from in and fans out to subscribers.
// perSinkBuffer is the channel buffer depth for each subscriber; minimum 1.
func NewBus(in <-chan detect.AttackEvent, perSinkBuffer int) *Bus {
	if perSinkBuffer < 1 {
		perSinkBuffer = 256
	}
	return &Bus{in: in, perSinkBuffer: perSinkBuffer}
}

// Subscribe registers a named subscriber and returns its dedicated channel.
// Must be called before Run.
func (b *Bus) Subscribe(name string) <-chan detect.AttackEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan detect.AttackEvent, b.perSinkBuffer)
	b.subs = append(b.subs, subscriber{name: name, ch: ch})
	return ch
}

// Run blocks until ctx is canceled or in is closed; fans out every event
// to all subscriber channels. Drops events non-blocking for slow subscribers.
func (b *Bus) Run(ctx context.Context) error {
	defer func() {
		b.mu.Lock()
		for _, s := range b.subs {
			close(s.ch)
		}
		b.subs = nil
		b.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-b.in:
			if !ok {
				return nil
			}
			b.mu.Lock()
			for _, s := range b.subs {
				select {
				case s.ch <- ev:
				default:
					slog.Warn("alert.bus: dropped event for slow subscriber",
						"sink", s.name,
						"incident_id", ev.IncidentID,
					)
				}
			}
			b.mu.Unlock()
		}
	}
}
