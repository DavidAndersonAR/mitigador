package detect_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/detect"
)

// helpers for state machine tests
var (
	testIP  = netip.MustParseAddr("10.0.0.1")
	testVec = detect.VectorUDPFlood
	testKey = detect.ExportKey(testIP, testVec)
	testTh  = detect.Threshold{
		HostgroupName: "test",
		Prefix:        netip.MustParsePrefix("10.0.0.0/8"),
		Vector:        detect.VectorUDPFlood,
		PPS:           100, BPS: 10_000,
		MinWindowSec: 5, GraceSec: 3,
	}
)

func makeInput(now time.Time, violating bool, dur int, pps, bps uint64) detect.Input {
	return detect.Input{
		Now:              now,
		CurrentPps:       pps,
		CurrentBps:       bps,
		AvgPpsLastWindow: pps,
		AvgBpsLastWindow: bps,
		Violating:        violating,
		DurationViolated: dur,
		Hostgroup:        "test",
		Confidence:       0.5,
	}
}

func TestSM_IdleToActive_RequiresMinWindow(t *testing.T) {
	sm := detect.NewStateMachine()
	now := time.Unix(1000, 0)

	// Feed 4 violating ticks (min_window=5) — no event yet.
	for i := 0; i < 4; i++ {
		in := makeInput(now.Add(time.Duration(i)*time.Second), true, i+1, 200, 20_000)
		ev := sm.Step(testKey, in, testTh)
		if ev != nil {
			t.Errorf("tick %d: expected no event before min_window, got %v", i, ev.State)
		}
	}

	// 5th tick — must emit StateStarted.
	in5 := makeInput(now.Add(4*time.Second), true, 5, 200, 20_000)
	ev := sm.Step(testKey, in5, testTh)
	if ev == nil {
		t.Fatal("expected StateStarted on 5th violating tick")
	}
	if ev.State != detect.StateStarted {
		t.Errorf("expected StateStarted, got %s", ev.State)
	}
}

func TestSM_ActiveToEnded_AfterGrace(t *testing.T) {
	sm := detect.NewStateMachine()
	now := time.Unix(2000, 0)

	// Get to ACTIVE (min_window=5 ticks).
	for i := 1; i <= 5; i++ {
		sm.Step(testKey, makeInput(now.Add(time.Duration(i)*time.Second), true, i, 200, 20_000), testTh)
	}

	// 3 non-violating ticks (grace_sec=3) → ENDED on 3rd.
	for i := 1; i <= 2; i++ {
		ev := sm.Step(testKey, makeInput(now.Add(time.Duration(5+i)*time.Second), false, 0, 10, 1000), testTh)
		if ev != nil {
			t.Errorf("non-violating tick %d: expected no event yet, got %s", i, ev.State)
		}
	}
	ev := sm.Step(testKey, makeInput(now.Add(8*time.Second), false, 0, 10, 1000), testTh)
	if ev == nil {
		t.Fatal("expected StateEnded after grace_sec=3 non-violating seconds")
	}
	if ev.State != detect.StateEnded {
		t.Errorf("expected StateEnded, got %s", ev.State)
	}
}

func TestSM_NonViolatingStreak_ResetByViolation(t *testing.T) {
	sm := detect.NewStateMachine()
	now := time.Unix(3000, 0)

	// Reach ACTIVE.
	for i := 1; i <= 5; i++ {
		sm.Step(testKey, makeInput(now.Add(time.Duration(i)*time.Second), true, i, 200, 20_000), testTh)
	}

	t6 := now.Add(6 * time.Second)
	t7 := now.Add(7 * time.Second)
	t8 := now.Add(8 * time.Second)
	t9 := now.Add(9 * time.Second)
	t10 := now.Add(10 * time.Second)

	// 2 non-violating ticks.
	sm.Step(testKey, makeInput(t6, false, 0, 10, 1000), testTh)
	sm.Step(testKey, makeInput(t7, false, 0, 10, 1000), testTh)

	// 1 violating — resets the no-violation streak.
	sm.Step(testKey, makeInput(t8, true, 1, 200, 20_000), testTh)

	// 2 more non-violating — streak count is 2, grace=3 → no ENDED yet.
	ev9 := sm.Step(testKey, makeInput(t9, false, 0, 10, 1000), testTh)
	if ev9 != nil && ev9.State == detect.StateEnded {
		t.Error("expected no ENDED after 2 non-violating (streak reset after violation)")
	}

	// 3rd consecutive non-violating → ENDED.
	ev10 := sm.Step(testKey, makeInput(t10, false, 0, 10, 1000), testTh)
	if ev10 == nil || ev10.State != detect.StateEnded {
		t.Errorf("expected StateEnded after 3 consecutive non-violating ticks, got %v", ev10)
	}
}

func TestSM_UpdateEmitted_OnPeakDouble(t *testing.T) {
	sm := detect.NewStateMachine()
	now := time.Unix(4000, 0)

	// Reach ACTIVE with pps=200.
	for i := 1; i <= 5; i++ {
		sm.Step(testKey, makeInput(now.Add(time.Duration(i)*time.Second), true, i, 200, 20_000), testTh)
	}

	// Grow peak to 450 (> 2×200=400) → expect UPDATED.
	in := makeInput(now.Add(6*time.Second), true, 6, 450, 45_000)
	ev := sm.Step(testKey, in, testTh)
	if ev == nil || ev.State != detect.StateUpdated {
		t.Errorf("expected StateUpdated when peak doubles, got %v", ev)
	}

	// Further peak growth — no more UPDATED (at most one).
	in2 := makeInput(now.Add(7*time.Second), true, 7, 1000, 100_000)
	ev2 := sm.Step(testKey, in2, testTh)
	if ev2 != nil && ev2.State == detect.StateUpdated {
		t.Error("expected no second StateUpdated")
	}
}

func TestSM_UpdateEmitted_AfterFiveMinutes(t *testing.T) {
	sm := detect.NewStateMachine()
	now := time.Unix(5000, 0)

	// Reach ACTIVE with pps=200.
	for i := 1; i <= 5; i++ {
		sm.Step(testKey, makeInput(now.Add(time.Duration(i)*time.Second), true, i, 200, 20_000), testTh)
	}

	// Tick at 5min+1s without peak doubling → expect UPDATED.
	t5m := now.Add(5*time.Minute + time.Second)
	in := makeInput(t5m, true, 301, 210, 21_000) // pps=210, not double of 200
	ev := sm.Step(testKey, in, testTh)
	if ev == nil || ev.State != detect.StateUpdated {
		t.Errorf("expected StateUpdated after 5min duration, got %v", ev)
	}
}

func TestSM_Cooldown_PreventsImmediateRetrigger(t *testing.T) {
	sm := detect.NewStateMachine()
	now := time.Unix(6000, 0)

	// Reach ACTIVE then ENDED.
	for i := 1; i <= 5; i++ {
		sm.Step(testKey, makeInput(now.Add(time.Duration(i)*time.Second), true, i, 200, 20_000), testTh)
	}
	// 3 non-violating → ENDED.
	for i := 1; i <= 3; i++ {
		sm.Step(testKey, makeInput(now.Add(time.Duration(5+i)*time.Second), false, 0, 10, 1000), testTh)
	}

	// Within 60s cooldown, violating again → no STARTED.
	t30 := now.Add(35 * time.Second) // still within 60s cooldown
	for i := 0; i < 10; i++ {
		ev := sm.Step(testKey, makeInput(t30.Add(time.Duration(i)*time.Second), true, i+5, 500, 50_000), testTh)
		if ev != nil && ev.State == detect.StateStarted {
			t.Errorf("unexpected StateStarted during cooldown at tick %d", i)
		}
	}

	// After 60s cooldown expires, violating again → new STARTED with new IncidentID.
	t70 := now.Add(70 * time.Second)
	var firstID string
	for i := 1; i <= 5; i++ {
		ev := sm.Step(testKey, makeInput(t70.Add(time.Duration(i)*time.Second), true, i, 500, 50_000), testTh)
		if ev != nil && ev.State == detect.StateStarted {
			firstID = ev.IncidentID
			break
		}
	}
	if firstID == "" {
		t.Fatal("expected new StateStarted after cooldown expires")
	}
}

func TestSM_IncidentID_StableAcrossEvents(t *testing.T) {
	sm := detect.NewStateMachine()
	now := time.Unix(7000, 0)

	// Reach ACTIVE — record IncidentID from STARTED.
	var startedID string
	for i := 1; i <= 5; i++ {
		ev := sm.Step(testKey, makeInput(now.Add(time.Duration(i)*time.Second), true, i, 200, 20_000), testTh)
		if ev != nil && ev.State == detect.StateStarted {
			startedID = ev.IncidentID
		}
	}
	if startedID == "" {
		t.Fatal("never got StateStarted")
	}

	// Trigger UPDATED (peak double).
	evUpd := sm.Step(testKey, makeInput(now.Add(6*time.Second), true, 6, 500, 50_000), testTh)
	if evUpd == nil || evUpd.State != detect.StateUpdated {
		t.Fatal("expected StateUpdated")
	}
	if evUpd.IncidentID != startedID {
		t.Errorf("UPDATED IncidentID mismatch: got %s, want %s", evUpd.IncidentID, startedID)
	}

	// Trigger ENDED (3 non-violating ticks).
	for i := 1; i <= 3; i++ {
		ev := sm.Step(testKey, makeInput(now.Add(time.Duration(6+i)*time.Second), false, 0, 10, 1000), testTh)
		if ev != nil && ev.State == detect.StateEnded {
			if ev.IncidentID != startedID {
				t.Errorf("ENDED IncidentID mismatch: got %s, want %s", ev.IncidentID, startedID)
			}
		}
	}
}
