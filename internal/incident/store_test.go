package incident_test

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mitigador/mitigador/internal/detect"
	"github.com/mitigador/mitigador/internal/incident"
	"github.com/mitigador/mitigador/internal/storage/postgres"
)

// testPool returns a *pgxpool.Pool connected to the test database,
// migrates schema, and registers t.Cleanup to close it.
// Skips if MITIGADOR_TEST_PG_DSN is unset.
func testPool(t *testing.T) *pgxpool.Pool {
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
	return pool
}

// makeEvent returns a minimal AttackEvent with the given state.
func makeEvent(id string, state detect.State, ip string, vec detect.Vector) detect.AttackEvent {
	addr, _ := netip.ParseAddr(ip)
	now := time.Now().UTC().Truncate(time.Millisecond)
	ev := detect.AttackEvent{
		IncidentID: id,
		State:      state,
		HostIP:     addr,
		Vector:     vec,
		Hostgroup:  "test-hg",
		Pps:        1000,
		Bps:        8_000_000,
		PeakPps:    1000,
		PeakBps:    8_000_000,
		Confidence: 0.7,
		StartedAt:  now,
		Now:        now,
	}
	return ev
}

// insertHostgroup inserts a hostgroup (for FK satisfaction) and returns its name.
func insertHostgroup(t *testing.T, pool *pgxpool.Pool, name, prefix string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO hostgroups (name, prefix) VALUES ($1, $2)
		 ON CONFLICT (name) DO NOTHING`, name, prefix)
	if err != nil {
		t.Fatalf("insertHostgroup: %v", err)
	}
}

// cleanIncidents deletes all test incidents by ID prefix (ULID).
func cleanIncident(t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), `DELETE FROM incidents WHERE id = $1`, id)
}

func TestStore_CreateInsertsIncidentAndUpdate(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)
	id := fmt.Sprintf("test-create-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanIncident(t, pool, id) })

	ev := makeEvent(id, detect.StateStarted, "10.0.0.1", detect.VectorUDPFlood)
	if err := s.Create(context.Background(), ev); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify incident row exists.
	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM incidents WHERE id=$1`, id).Scan(&count); err != nil {
		t.Fatalf("query incident: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 incident row, got %d", count)
	}

	// Verify attack_updates row exists with kind='started'.
	var kind string
	if err := pool.QueryRow(context.Background(),
		`SELECT kind FROM attack_updates WHERE incident_id=$1`, id).Scan(&kind); err != nil {
		t.Fatalf("query attack_updates: %v", err)
	}
	if kind != "started" {
		t.Errorf("expected kind='started', got %q", kind)
	}
}

func TestStore_Update_BumpsPeaks(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)
	id := fmt.Sprintf("test-update-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanIncident(t, pool, id) })

	ev := makeEvent(id, detect.StateStarted, "10.0.0.2", detect.VectorUDPFlood)
	ev.PeakPps = 1000
	ev.PeakBps = 8_000_000
	if err := s.Create(context.Background(), ev); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update with higher peaks.
	ev2 := ev
	ev2.State = detect.StateUpdated
	ev2.Pps = 5000
	ev2.Bps = 40_000_000
	ev2.PeakPps = 5000
	ev2.PeakBps = 40_000_000
	ev2.Confidence = 0.9
	ev2.Now = ev.Now.Add(10 * time.Second)
	if err := s.Update(context.Background(), ev2); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var peakPps, peakBps uint64
	if err := pool.QueryRow(context.Background(),
		`SELECT peak_pps, peak_bps FROM incidents WHERE id=$1`, id).Scan(&peakPps, &peakBps); err != nil {
		t.Fatalf("query: %v", err)
	}
	if peakPps != 5000 {
		t.Errorf("peak_pps: want 5000, got %d", peakPps)
	}
	if peakBps != 40_000_000 {
		t.Errorf("peak_bps: want 40000000, got %d", peakBps)
	}
}

func TestStore_End_SetsEndedAt(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)
	id := fmt.Sprintf("test-end-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanIncident(t, pool, id) })

	ev := makeEvent(id, detect.StateStarted, "10.0.0.3", detect.VectorUDPFlood)
	if err := s.Create(context.Background(), ev); err != nil {
		t.Fatalf("Create: %v", err)
	}

	endTime := ev.Now.Add(30 * time.Second)
	evEnd := ev
	evEnd.State = detect.StateEnded
	evEnd.EndedAt = endTime
	evEnd.Now = endTime
	if err := s.End(context.Background(), evEnd); err != nil {
		t.Fatalf("End: %v", err)
	}

	var endedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT ended_at FROM incidents WHERE id=$1`, id).Scan(&endedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if endedAt == nil {
		t.Fatal("ended_at is still NULL after End()")
	}
}

func TestStore_List_FiltersByVector(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)

	idUDP := fmt.Sprintf("test-vecudp-%d", time.Now().UnixNano())
	idICMP := fmt.Sprintf("test-vicicmp-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanIncident(t, pool, idUDP)
		cleanIncident(t, pool, idICMP)
	})

	evUDP := makeEvent(idUDP, detect.StateStarted, "10.1.0.1", detect.VectorUDPFlood)
	if err := s.Create(context.Background(), evUDP); err != nil {
		t.Fatalf("Create UDP: %v", err)
	}
	evICMP := makeEvent(idICMP, detect.StateStarted, "10.1.0.2", detect.VectorICMPFlood)
	if err := s.Create(context.Background(), evICMP); err != nil {
		t.Fatalf("Create ICMP: %v", err)
	}

	vec := detect.VectorUDPFlood
	res, err := s.List(context.Background(), incident.Filter{Vector: &vec, Limit: 500})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, inc := range res.Items {
		if inc.ID == idICMP {
			t.Errorf("ICMP incident leaked into UDP filter results")
		}
	}
	found := false
	for _, inc := range res.Items {
		if inc.ID == idUDP {
			found = true
		}
	}
	if !found {
		t.Error("UDP incident not returned in UDP filter results")
	}
}

func TestStore_List_FiltersByHostIP(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)

	id1 := fmt.Sprintf("test-ip1-%d", time.Now().UnixNano())
	id2 := fmt.Sprintf("test-ip2-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanIncident(t, pool, id1)
		cleanIncident(t, pool, id2)
	})

	ev1 := makeEvent(id1, detect.StateStarted, "10.2.0.1", detect.VectorUDPFlood)
	ev2 := makeEvent(id2, detect.StateStarted, "10.2.0.2", detect.VectorUDPFlood)
	if err := s.Create(context.Background(), ev1); err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	if err := s.Create(context.Background(), ev2); err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	ip, _ := netip.ParseAddr("10.2.0.1")
	res, err := s.List(context.Background(), incident.Filter{HostIP: &ip, Limit: 500})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, inc := range res.Items {
		if inc.ID == id2 {
			t.Error("second IP leaked into host IP filter results")
		}
	}
	found := false
	for _, inc := range res.Items {
		if inc.ID == id1 {
			found = true
		}
	}
	if !found {
		t.Error("first IP incident not returned in host IP filter results")
	}
}

func TestStore_List_FiltersBySinceUntil(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)

	now := time.Now().UTC().Truncate(time.Millisecond)
	idOld := fmt.Sprintf("test-old-%d", time.Now().UnixNano())
	idNew := fmt.Sprintf("test-new-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanIncident(t, pool, idOld)
		cleanIncident(t, pool, idNew)
	})

	evOld := makeEvent(idOld, detect.StateStarted, "10.3.0.1", detect.VectorUDPFlood)
	evOld.StartedAt = now.Add(-2 * time.Hour)
	evOld.Now = evOld.StartedAt
	if err := s.Create(context.Background(), evOld); err != nil {
		t.Fatalf("Create old: %v", err)
	}

	evNew := makeEvent(idNew, detect.StateStarted, "10.3.0.2", detect.VectorUDPFlood)
	evNew.StartedAt = now.Add(-30 * time.Minute)
	evNew.Now = evNew.StartedAt
	if err := s.Create(context.Background(), evNew); err != nil {
		t.Fatalf("Create new: %v", err)
	}

	since := now.Add(-1 * time.Hour)
	res, err := s.List(context.Background(), incident.Filter{Since: &since, Limit: 500})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, inc := range res.Items {
		if inc.ID == idOld {
			t.Error("old incident appeared in Since filter results")
		}
	}
	found := false
	for _, inc := range res.Items {
		if inc.ID == idNew {
			found = true
		}
	}
	if !found {
		t.Error("new incident not returned in Since filter results")
	}
}

func TestStore_List_PaginatesAndOrdersByStartedAtDESC(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)

	base := time.Now().UTC().Truncate(time.Millisecond)
	var ids []string
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("test-page-%d-%d", i, time.Now().UnixNano())
		ids = append(ids, id)
		ev := makeEvent(id, detect.StateStarted, fmt.Sprintf("10.4.0.%d", i+1), detect.VectorUDPFlood)
		ev.StartedAt = base.Add(time.Duration(i) * time.Second)
		ev.Now = ev.StartedAt
		if err := s.Create(context.Background(), ev); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		for _, id := range ids {
			cleanIncident(t, pool, id)
		}
	})

	since := base.Add(-time.Second)
	res, err := s.List(context.Background(), incident.Filter{Since: &since, Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if len(res.Items) != 2 {
		t.Errorf("page 1: want 2 items, got %d", len(res.Items))
	}
	if res.Total < 3 {
		t.Errorf("Total: want >= 3, got %d", res.Total)
	}
	// Items should be ordered started_at DESC — newest first.
	if len(res.Items) >= 2 {
		if !res.Items[0].StartedAt.After(res.Items[1].StartedAt) &&
			!res.Items[0].StartedAt.Equal(res.Items[1].StartedAt) {
			t.Errorf("items not ordered DESC: %v, %v", res.Items[0].StartedAt, res.Items[1].StartedAt)
		}
	}
}

func TestStore_Get_ReturnsIncidentAndUpdates(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)
	id := fmt.Sprintf("test-get-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanIncident(t, pool, id) })

	ev := makeEvent(id, detect.StateStarted, "10.5.0.1", detect.VectorUDPFlood)
	if err := s.Create(context.Background(), ev); err != nil {
		t.Fatalf("Create: %v", err)
	}

	evU := ev
	evU.State = detect.StateUpdated
	evU.Now = ev.Now.Add(5 * time.Second)
	if err := s.Update(context.Background(), evU); err != nil {
		t.Fatalf("Update: %v", err)
	}

	inc, updates, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if inc == nil {
		t.Fatal("Get returned nil incident")
	}
	if inc.ID != id {
		t.Errorf("ID: want %q, got %q", id, inc.ID)
	}
	if len(updates) != 2 {
		t.Errorf("want 2 updates (started + update), got %d", len(updates))
	}
	if len(updates) >= 1 && updates[0].Kind != "started" {
		t.Errorf("first update kind: want 'started', got %q", updates[0].Kind)
	}
}

func TestStore_Get_ReturnsErrNotFoundForMissing(t *testing.T) {
	pool := testPool(t)
	s := incident.NewStore(pool)

	_, _, err := s.Get(context.Background(), "nonexistent-id-xyzzy")
	if err == nil {
		t.Fatal("expected ErrNotFound, got nil")
	}
	if !isErrNotFound(err) {
		t.Errorf("expected incident.ErrNotFound, got %v", err)
	}
}

func TestStore_ListActive_OnlyOpen(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)

	idOpen := fmt.Sprintf("test-open-%d", time.Now().UnixNano())
	idClosed := fmt.Sprintf("test-closed-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanIncident(t, pool, idOpen)
		cleanIncident(t, pool, idClosed)
	})

	evOpen := makeEvent(idOpen, detect.StateStarted, "10.6.0.1", detect.VectorUDPFlood)
	if err := s.Create(context.Background(), evOpen); err != nil {
		t.Fatalf("Create open: %v", err)
	}

	evClosed := makeEvent(idClosed, detect.StateStarted, "10.6.0.2", detect.VectorUDPFlood)
	if err := s.Create(context.Background(), evClosed); err != nil {
		t.Fatalf("Create closed: %v", err)
	}
	evEnd := evClosed
	evEnd.State = detect.StateEnded
	evEnd.EndedAt = evClosed.Now.Add(10 * time.Second)
	evEnd.Now = evEnd.EndedAt
	if err := s.End(context.Background(), evEnd); err != nil {
		t.Fatalf("End closed: %v", err)
	}

	active, err := s.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, inc := range active {
		if inc.ID == idClosed {
			t.Error("closed incident appeared in ListActive results")
		}
	}
	found := false
	for _, inc := range active {
		if inc.ID == idOpen {
			found = true
		}
	}
	if !found {
		t.Error("open incident not found in ListActive results")
	}
}

func TestStore_CloseOrphans_AffectsOldOpenIncidents(t *testing.T) {
	pool := testPool(t)
	insertHostgroup(t, pool, "test-hg", "10.0.0.0/8")
	s := incident.NewStore(pool)

	idOld := fmt.Sprintf("test-orphan-old-%d", time.Now().UnixNano())
	idRecent := fmt.Sprintf("test-orphan-recent-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanIncident(t, pool, idOld)
		cleanIncident(t, pool, idRecent)
	})

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Old open incident: created (and started_at) 25 hours ago.
	evOld := makeEvent(idOld, detect.StateStarted, "10.7.0.1", detect.VectorUDPFlood)
	evOld.StartedAt = now.Add(-25 * time.Hour)
	evOld.Now = evOld.StartedAt
	if err := s.Create(context.Background(), evOld); err != nil {
		t.Fatalf("Create old: %v", err)
	}
	// Manually set created_at to 25h ago so CloseOrphans triggers.
	if _, err := pool.Exec(context.Background(),
		`UPDATE incidents SET created_at=$1 WHERE id=$2`,
		now.Add(-25*time.Hour), idOld); err != nil {
		t.Fatalf("set created_at: %v", err)
	}

	// Recent open incident: created 30 minutes ago — should NOT be closed.
	evRecent := makeEvent(idRecent, detect.StateStarted, "10.7.0.2", detect.VectorUDPFlood)
	evRecent.StartedAt = now.Add(-30 * time.Minute)
	evRecent.Now = evRecent.StartedAt
	if err := s.Create(context.Background(), evRecent); err != nil {
		t.Fatalf("Create recent: %v", err)
	}

	cutoff := now.Add(-24 * time.Hour)
	n, err := s.CloseOrphans(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("CloseOrphans: %v", err)
	}
	if n < 1 {
		t.Errorf("CloseOrphans: want >= 1 affected, got %d", n)
	}

	// Old incident should now have ended_at set.
	var endedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT ended_at FROM incidents WHERE id=$1`, idOld).Scan(&endedAt); err != nil {
		t.Fatalf("query old ended_at: %v", err)
	}
	if endedAt == nil {
		t.Error("old orphan incident: ended_at still NULL after CloseOrphans")
	}

	// Recent incident should remain open.
	var recentEndedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT ended_at FROM incidents WHERE id=$1`, idRecent).Scan(&recentEndedAt); err != nil {
		t.Fatalf("query recent ended_at: %v", err)
	}
	if recentEndedAt != nil {
		t.Error("recent incident was wrongly closed by CloseOrphans")
	}
}

func TestStore_Create_RequiresExistingHostgroup_OrNullHostgroupID(t *testing.T) {
	pool := testPool(t)
	// Do NOT insert a hostgroup for this test — we pass a nonexistent hostgroup name.
	s := incident.NewStore(pool)
	id := fmt.Sprintf("test-nohg-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanIncident(t, pool, id) })

	ev := makeEvent(id, detect.StateStarted, "10.8.0.1", detect.VectorUDPFlood)
	ev.Hostgroup = "nonexistent-hostgroup-xyzzy"
	if err := s.Create(context.Background(), ev); err != nil {
		t.Fatalf("Create with missing hostgroup should succeed (null FK): %v", err)
	}

	// Verify hostgroup_id is NULL (subquery returned no rows).
	var hgID *int64
	if err := pool.QueryRow(context.Background(),
		`SELECT hostgroup_id FROM incidents WHERE id=$1`, id).Scan(&hgID); err != nil {
		t.Fatalf("query: %v", err)
	}
	if hgID != nil {
		t.Errorf("expected NULL hostgroup_id for nonexistent hostgroup, got %v", *hgID)
	}
}

// isErrNotFound checks whether the error is incident.ErrNotFound.
func isErrNotFound(err error) bool {
	return err != nil && err.Error() == "incident: not found"
}
