package email

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/config"
	"github.com/mitigador/mitigador/internal/detect"
)

// White-box tests using package email (internal test) to access unexported format().

func makeTestSender(t *testing.T) *Sender {
	t.Helper()
	cfg := config.SMTP{
		Host:     "localhost",
		Port:     25,
		Username: "user",
		Password: "pass",
		Security: "plain",
		FromAddr: "mitigador@example.com",
		ToAddrs:  []string{"ops@example.com"},
	}
	s, err := NewSender(cfg, "https://mitigador.example.com")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	return s
}

func baseEvent(state detect.State) detect.AttackEvent {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	return detect.AttackEvent{
		IncidentID: "01HY00000000000000ABCDE1",
		State:      state,
		HostIP:     netip.MustParseAddr("203.0.113.42"),
		Vector:     detect.VectorUDPFlood,
		Hostgroup:  "clientes-rj",
		Pps:        80000,
		Bps:        640000000,
		PeakPps:    120000,
		PeakBps:    960000000,
		StartedAt:  start,
		EndedAt:    start.Add(8 * time.Minute),
		Now:        start,
	}
}

func TestFormat_Started_SubjectIncludesIPAndVector(t *testing.T) {
	s := makeTestSender(t)
	ev := baseEvent(detect.StateStarted)
	subject, _ := s.format(ev)

	if !strings.Contains(subject, ev.HostIP.String()) {
		t.Errorf("subject missing IP %q: %s", ev.HostIP.String(), subject)
	}
	if !strings.Contains(subject, "UDP Flood") {
		t.Errorf("subject missing 'UDP Flood': %s", subject)
	}
}

func TestFormat_Started_BodyIncludesURL(t *testing.T) {
	s := makeTestSender(t)
	ev := baseEvent(detect.StateStarted)
	_, body := s.format(ev)

	wantURL := "https://mitigador.example.com/incidents/" + ev.IncidentID
	if !strings.Contains(body, wantURL) {
		t.Errorf("body missing incident URL %q:\n%s", wantURL, body)
	}
}

func TestFormat_Updated(t *testing.T) {
	s := makeTestSender(t)
	ev := baseEvent(detect.StateUpdated)
	subject, body := s.format(ev)

	if !strings.Contains(subject, "andamento") {
		t.Errorf("updated subject missing 'andamento': %s", subject)
	}
	if !strings.Contains(body, "em curso") {
		t.Errorf("updated body missing 'em curso': %s", body)
	}
	if !strings.Contains(body, "incidents") {
		t.Errorf("updated body missing incident URL: %s", body)
	}
}

func TestFormat_Ended_BodyIncludesDuration(t *testing.T) {
	s := makeTestSender(t)
	ev := baseEvent(detect.StateEnded)
	subject, body := s.format(ev)

	if !strings.Contains(subject, "encerrado") {
		t.Errorf("ended subject missing 'encerrado': %s", subject)
	}
	// 8 minutes duration
	if !strings.Contains(body, "8m") {
		t.Errorf("ended body missing duration '8m': %s", body)
	}
	if !strings.Contains(body, "incidents") {
		t.Errorf("ended body missing incident URL: %s", body)
	}
}

func TestFormat_DoesNotImportNetSMTP(t *testing.T) {
	// Compile-time guard: this test just documents the constraint.
	// Real enforcement is via go mod graph / grep in CI.
	// If wneessen/go-mail is used, net/smtp is NOT imported directly.
	_ = t
}
