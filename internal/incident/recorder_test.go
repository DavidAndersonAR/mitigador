package incident_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/detect"
	"github.com/mitigador/mitigador/internal/incident"
	"github.com/mitigador/mitigador/internal/storage/postgres"
)

// recorderTestStore returns a Store backed by a freshly migrated test pool.
// Skips if MITIGADOR_TEST_PG_DSN is not set.
func recorderTestStore(t *testing.T) *incident.Store {
	t.Helper()
	dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MITIGADOR_TEST_PG_DSN not set")
	}
	if err := postgres.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := postgres.NewPool(context.Background(), dsn, 4, 1)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	// Ensure test-hg hostgroup exists.
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO hostgroups (name, prefix) VALUES ('test-hg', '10.0.0.0/8') ON CONFLICT (name) DO NOTHING`)
	return incident.NewStore(pool)
}

func TestRecorder_StartedCallsCreate(t *testing.T) {
	store := recorderTestStore(t)
	ch := make(chan detect.AttackEvent, 8)

	rec := incident.NewRecorder(store, ch)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- rec.Run(ctx) }()

	id := fmt.Sprintf("rec-started-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		// Use exported pool via the store indirectly — get pool from testPool helper not reachable here.
		// Cleanup via direct SQL; capture DSN again.
		dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
		pool, _ := postgres.NewPool(context.Background(), dsn, 1, 1)
		if pool != nil {
			pool.Exec(context.Background(), `DELETE FROM incidents WHERE id=$1`, id) //nolint:errcheck
			pool.Close()
		}
	})

	ev := makeEvent(id, detect.StateStarted, "10.0.1.1", detect.VectorUDPFlood)
	ch <- ev

	// Allow time for goroutine to process.
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	inc, _, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get after Started: %v", err)
	}
	if inc == nil {
		t.Fatal("expected incident to exist after StateStarted")
	}
	if inc.ID != id {
		t.Errorf("ID: want %q, got %q", id, inc.ID)
	}
}

func TestRecorder_EndedCallsEnd(t *testing.T) {
	store := recorderTestStore(t)
	ch := make(chan detect.AttackEvent, 8)

	rec := incident.NewRecorder(store, ch)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- rec.Run(ctx) }()

	id := fmt.Sprintf("rec-ended-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
		pool, _ := postgres.NewPool(context.Background(), dsn, 1, 1)
		if pool != nil {
			pool.Exec(context.Background(), `DELETE FROM incidents WHERE id=$1`, id) //nolint:errcheck
			pool.Close()
		}
	})

	now := time.Now().UTC()
	evStart := makeEvent(id, detect.StateStarted, "10.0.1.2", detect.VectorUDPFlood)
	evStart.StartedAt = now
	evStart.Now = now
	ch <- evStart

	time.Sleep(100 * time.Millisecond)

	evEnd := evStart
	evEnd.State = detect.StateEnded
	evEnd.EndedAt = now.Add(30 * time.Second)
	evEnd.Now = evEnd.EndedAt
	ch <- evEnd

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	inc, _, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get after Ended: %v", err)
	}
	if inc.EndedAt == nil {
		t.Fatal("ended_at should be set after StateEnded")
	}
}

func TestRecorder_DBErrorDoesNotHaltLoop(t *testing.T) {
	// This test verifies that when an event causes a DB error (e.g. duplicate insert
	// with ON CONFLICT DO NOTHING still succeeds, so we force an error by sending
	// StateUpdated for a non-existent incident — the UPDATE will affect 0 rows which
	// is not an error in pgx; instead we send StateStarted with invalid vector which
	// violates the CHECK constraint).
	// Strategy: send a StateStarted with an invalid vector (DB rejects it),
	// then send a valid StateStarted — both should be processed without panic.
	dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MITIGADOR_TEST_PG_DSN not set")
	}
	if err := postgres.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := postgres.NewPool(context.Background(), dsn, 4, 1)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO hostgroups (name, prefix) VALUES ('test-hg', '10.0.0.0/8') ON CONFLICT (name) DO NOTHING`)

	store := incident.NewStore(pool)
	ch := make(chan detect.AttackEvent, 8)
	rec := incident.NewRecorder(store, ch)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rec.Run(ctx) }()

	// Event 1: invalid vector — should fail DB CHECK constraint, but recorder must not halt.
	idBad := fmt.Sprintf("rec-bad-%d", time.Now().UnixNano())
	evBad := makeEvent(idBad, detect.StateStarted, "10.0.2.1", detect.VectorUDPFlood)
	evBad.Vector = detect.Vector("invalid_vector") // violates CHECK constraint
	ch <- evBad

	time.Sleep(100 * time.Millisecond)

	// Event 2: valid event — should be processed successfully.
	idGood := fmt.Sprintf("rec-good-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM incidents WHERE id=$1`, idGood) //nolint:errcheck
	})
	evGood := makeEvent(idGood, detect.StateStarted, "10.0.2.2", detect.VectorUDPFlood)
	ch <- evGood

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recorder.Run did not return within 3s after cancel")
	}

	// Good incident must be persisted despite the earlier error.
	inc, _, err := store.Get(context.Background(), idGood)
	if err != nil {
		t.Fatalf("Get good incident: %v (recorder halted on DB error)", err)
	}
	if inc == nil {
		t.Fatal("good incident not found — recorder appears to have halted on DB error")
	}
}

func TestRecorder_DrainsRemainingOnCancel(t *testing.T) {
	store := recorderTestStore(t)
	ch := make(chan detect.AttackEvent, 8)
	rec := incident.NewRecorder(store, ch)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rec.Run(ctx) }()

	now := time.Now().UTC()
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		ids[i] = fmt.Sprintf("rec-drain-%d-%d", i, time.Now().UnixNano())
	}
	t.Cleanup(func() {
		dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
		pool, _ := postgres.NewPool(context.Background(), dsn, 1, 1)
		if pool != nil {
			for _, id := range ids {
				pool.Exec(context.Background(), `DELETE FROM incidents WHERE id=$1`, id) //nolint:errcheck
			}
			pool.Close()
		}
	})

	// Send 3 events then immediately cancel.
	for i, id := range ids {
		ev := makeEvent(id, detect.StateStarted, fmt.Sprintf("10.0.3.%d", i+1), detect.VectorUDPFlood)
		ev.StartedAt = now
		ev.Now = now
		ch <- ev
	}
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("recorder.Run did not return within 5s")
	}

	// All 3 events should have been drained and persisted.
	for _, id := range ids {
		inc, _, err := store.Get(context.Background(), id)
		if err != nil {
			t.Errorf("Get %q: %v (event may not have been drained)", id, err)
			continue
		}
		if inc == nil {
			t.Errorf("incident %q not found after drain", id)
		}
	}
}
