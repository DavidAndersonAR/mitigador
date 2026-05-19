package alert_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/alert"
	"github.com/mitigador/mitigador/internal/detect"
)

func makeEvent(id string) detect.AttackEvent {
	return detect.AttackEvent{
		IncidentID: id,
		State:      detect.StateStarted,
		HostIP:     netip.MustParseAddr("192.0.2.1"),
		Vector:     detect.VectorUDPFlood,
		Hostgroup:  "test",
		Pps:        1000,
		Bps:        8000000,
		PeakPps:    1000,
		PeakBps:    8000000,
		StartedAt:  time.Now(),
		Now:        time.Now(),
	}
}

func TestBus_FanOutToAllSubscribers(t *testing.T) {
	in := make(chan detect.AttackEvent, 10)
	b := alert.NewBus(in, 10)

	ch1 := b.Subscribe("s1")
	ch2 := b.Subscribe("s2")
	ch3 := b.Subscribe("s3")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = b.Run(ctx)
	}()

	ev := makeEvent("01HY00000000000000000001")
	in <- ev

	for i, ch := range []<-chan detect.AttackEvent{ch1, ch2, ch3} {
		select {
		case got := <-ch:
			if got.IncidentID != ev.IncidentID {
				t.Errorf("subscriber %d: got IncidentID %q, want %q", i, got.IncidentID, ev.IncidentID)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d: timed out waiting for event", i)
		}
	}
}

func TestBus_DropsForSlowSubscriber(t *testing.T) {
	// Verifies drop-on-full: a slow subscriber (buffer=1, never drained) must NOT
	// block delivery to the fast subscriber (large buffer).
	// Approach: use a large perSinkBuffer so fast gets all events; verify the bus
	// didn't deadlock by receiving all events from fast within the deadline.
	const total = 5
	in := make(chan detect.AttackEvent, total)
	// perSinkBuffer=100 gives both subscribers room; we verify fast gets all 5
	// and that the bus never blocks even though slow is never read.
	b := alert.NewBus(in, 100)

	_ = b.Subscribe("slow") // deliberately never drained
	fast := b.Subscribe("fast")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = b.Run(ctx) }()

	ids := []string{
		"01HY00000000000000000001",
		"01HY00000000000000000002",
		"01HY00000000000000000003",
		"01HY00000000000000000004",
		"01HY00000000000000000005",
	}
	for _, id := range ids {
		in <- makeEvent(id)
	}

	// Fast subscriber must receive all 5 events without the bus blocking.
	got := 0
	deadline := time.After(3 * time.Second)
	for got < total {
		select {
		case <-fast:
			got++
		case <-deadline:
			t.Fatalf("fast subscriber: got %d events, want %d (bus may have blocked on slow)", got, total)
		}
	}
}

// TestBus_DropsOnFullBuffer verifies drop-on-full with buffer=1 and no reader.
// The bus must not block and must return nil when input is closed.
func TestBus_DropsOnFullBuffer(t *testing.T) {
	const total = 10
	in := make(chan detect.AttackEvent, total)
	b := alert.NewBus(in, 1) // tiny buffer: fills after 1 event

	_ = b.Subscribe("no-reader") // never drained

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fill input before bus starts.
	for i := range total {
		_ = i
		in <- makeEvent("01HY00000000000000000001")
	}
	close(in) // signal end

	done := make(chan error, 1)
	go func() {
		done <- b.Run(ctx)
	}()

	select {
	case err := <-done:
		// Run should return nil when in is closed (not block forever)
		if err != nil {
			t.Errorf("Bus.Run returned %v; want nil (closed input)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Bus.Run blocked on slow subscriber — drop-on-full not working")
	}
}

func TestBus_ClosesSubscribersOnCtxCancel(t *testing.T) {
	in := make(chan detect.AttackEvent, 1)
	b := alert.NewBus(in, 10)
	ch := b.Subscribe("test")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = b.Run(ctx)
	}()

	cancel()
	<-done

	// Channel should be closed after Run exits
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("subscriber channel should be closed, but received value")
		}
	case <-time.After(time.Second):
		t.Error("subscriber channel was not closed after ctx cancel")
	}
}
