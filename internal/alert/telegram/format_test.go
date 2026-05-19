package telegram_test

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/alert/telegram"
	"github.com/mitigador/mitigador/internal/detect"
)

const testAppURL = "https://mitigador.example.com"

func makeEv(state detect.State) detect.AttackEvent {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return detect.AttackEvent{
		IncidentID: "01HY000000000000000ABCDE",
		State:      state,
		HostIP:     netip.MustParseAddr("192.0.2.1"),
		Vector:     detect.VectorUDPFlood,
		Hostgroup:  "clientes-sp",
		Pps:        50000,
		Bps:        400000000,
		PeakPps:    75000,
		PeakBps:    600000000,
		Confidence: 0.92,
		StartedAt:  start,
		EndedAt:    start.Add(5 * time.Minute),
		Now:        start,
	}
}

func TestFormat_Started_ContainsAllFields(t *testing.T) {
	ev := makeEv(detect.StateStarted)
	out := telegram.Format(ev, testAppURL)

	checks := []string{
		"Ataque detectado",
		"192",     // IP (may be escaped)
		"UDP Flood",
		"50",      // pps value
		"Mbps",    // bps human-readable
		"incidents",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("Format(StateStarted): missing %q in output:\n%s", want, out)
		}
	}
}

func TestFormat_Updated(t *testing.T) {
	ev := makeEv(detect.StateUpdated)
	out := telegram.Format(ev, testAppURL)

	if !strings.Contains(out, "ainda em curso") {
		t.Errorf("Format(StateUpdated): expected 'ainda em curso', got:\n%s", out)
	}
	if !strings.Contains(out, "Pico") {
		t.Errorf("Format(StateUpdated): expected 'Pico', got:\n%s", out)
	}
	if !strings.Contains(out, "incidents") {
		t.Errorf("Format(StateUpdated): missing incidents URL, got:\n%s", out)
	}
}

func TestFormat_Ended_IncludesDuration(t *testing.T) {
	ev := makeEv(detect.StateEnded)
	out := telegram.Format(ev, testAppURL)

	if !strings.Contains(out, "Ataque encerrado") {
		t.Errorf("Format(StateEnded): expected 'Ataque encerrado', got:\n%s", out)
	}
	// 5 minutes duration should appear as "5m0s"
	if !strings.Contains(out, "5m") {
		t.Errorf("Format(StateEnded): expected duration containing '5m', got:\n%s", out)
	}
}

func TestFormat_EscapesMarkdownV2Chars(t *testing.T) {
	ev := makeEv(detect.StateStarted)
	out := telegram.Format(ev, testAppURL)

	// IP "192.0.2.1" has dots — they should be escaped as "\."
	if !strings.Contains(out, `\.`) {
		t.Errorf("Format: expected escaped dots in output (MarkdownV2), got:\n%s", out)
	}
}

func TestFormat_LinksToIncidentURL(t *testing.T) {
	ev := makeEv(detect.StateStarted)
	out := telegram.Format(ev, testAppURL)

	// The URL is mdv2-escaped: "https://mitigador\.example\.com/incidents/01HY000000000000000ABCDE"
	// Check the base structure is present
	if !strings.Contains(out, "mitigador") {
		t.Errorf("Format: expected app base URL in output, got:\n%s", out)
	}
	if !strings.Contains(out, "01HY000000000000000ABCDE") {
		t.Errorf("Format: expected incident ID in output, got:\n%s", out)
	}
}
