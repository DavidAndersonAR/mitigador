// Package email provides an SMTP alert Sink for Mitigador via wneessen/go-mail.
package email

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mail "github.com/wneessen/go-mail"

	"github.com/mitigador/mitigador/internal/config"
	"github.com/mitigador/mitigador/internal/detect"
)

// Sender implements alert.Sink for SMTP email.
// It sends a pt-BR plain-text email for every AttackEvent received on its
// input channel. Each email includes the incident URL for operator follow-up.
// Cadence matches Telegram (D-17) because both subscribe to the same Bus.
type Sender struct {
	client *mail.Client
	from   string
	to     []string
	appURL string
}

// NewSender creates an SMTP Sender. Security must be one of "starttls", "tls",
// or "plain". It validates the security mode but does NOT open a connection at
// construction time — DialAndSendWithContext opens and closes per send.
func NewSender(cfg config.SMTP, appBaseURL string) (*Sender, error) {
	opts := []mail.Option{
		mail.WithPort(cfg.Port),
		mail.WithSMTPAuth(mail.SMTPAuthPlain),
	}

	// Only add credentials if they are provided (some servers allow anonymous
	// plain connections or use IP allow-listing).
	if cfg.Username != "" {
		opts = append(opts, mail.WithUsername(cfg.Username))
	}
	if cfg.Password != "" {
		opts = append(opts, mail.WithPassword(cfg.Password))
	}

	switch cfg.Security {
	case "starttls":
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	case "tls":
		// Implicit TLS (SMTPS, typically port 465). WithSSLPort overrides the
		// port to 465 unless WithPort was already set — WithPort is set above
		// so the operator's configured port takes precedence.
		opts = append(opts, mail.WithSSLPort(false))
	case "plain":
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	default:
		return nil, fmt.Errorf("email: unknown security mode %q (want starttls|tls|plain)", cfg.Security)
	}

	c, err := mail.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("email: new client: %w", err)
	}

	return &Sender{
		client: c,
		from:   cfg.FromAddr,
		to:     append([]string(nil), cfg.ToAddrs...),
		appURL: appBaseURL,
	}, nil
}

// Name satisfies alert.Sink.
func (s *Sender) Name() string { return "email" }

// Run satisfies alert.Sink. It blocks until ctx is done or in is closed,
// sending one email per AttackEvent. Send failures are logged but do not
// stop the loop — parity with Telegram's resilience pattern.
func (s *Sender) Run(ctx context.Context, in <-chan detect.AttackEvent) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				return nil
			}
			if err := s.sendOne(ctx, ev); err != nil {
				slog.Error("email: send failed",
					"incident_id", ev.IncidentID,
					"state", string(ev.State),
					"err", err.Error(),
				)
			}
		}
	}
}

func (s *Sender) sendOne(ctx context.Context, ev detect.AttackEvent) error {
	msg := mail.NewMsg()
	if err := msg.From(s.from); err != nil {
		return fmt.Errorf("email: from address: %w", err)
	}
	if err := msg.To(s.to...); err != nil {
		return fmt.Errorf("email: to address: %w", err)
	}

	subject, body := s.format(ev)
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, body)

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return s.client.DialAndSendWithContext(sendCtx, msg)
}

// format returns the pt-BR subject and plain-text body for ev.
// The body always includes the incident URL for operator follow-up (ALER-05).
func (s *Sender) format(ev detect.AttackEvent) (subject, body string) {
	vec := vectorLabel(ev.Vector)
	ip := ev.HostIP.String()
	url := fmt.Sprintf("%s/incidents/%s", strings.TrimRight(s.appURL, "/"), ev.IncidentID)

	switch ev.State {
	case detect.StateStarted:
		subject = fmt.Sprintf("[Mitigador] Ataque detectado em %s (%s)", ip, vec)
		body = fmt.Sprintf(
			"Ataque detectado.\n\n"+
				"IP alvo:   %s\n"+
				"Vetor:     %s\n"+
				"Taxa:      %d pps / %d bps\n"+
				"Hostgroup: %s\n"+
				"Iniciado:  %s\n"+
				"Incidente: %s\n"+
				"Detalhes:  %s\n",
			ip, vec, ev.Pps, ev.Bps, ev.Hostgroup,
			ev.StartedAt.UTC().Format(time.RFC3339),
			ev.IncidentID, url,
		)

	case detect.StateUpdated:
		subject = fmt.Sprintf("[Mitigador] Ataque em andamento em %s (%s)", ip, vec)
		body = fmt.Sprintf(
			"Ataque ainda em curso.\n\n"+
				"IP alvo:   %s\n"+
				"Vetor:     %s\n"+
				"Pico:      %d pps / %d bps\n"+
				"Incidente: %s\n"+
				"Detalhes:  %s\n",
			ip, vec, ev.PeakPps, ev.PeakBps,
			ev.IncidentID, url,
		)

	case detect.StateEnded:
		dur := ev.EndedAt.Sub(ev.StartedAt).Round(time.Second)
		subject = fmt.Sprintf("[Mitigador] Ataque encerrado em %s (%s)", ip, vec)
		body = fmt.Sprintf(
			"Ataque encerrado.\n\n"+
				"IP alvo:   %s\n"+
				"Vetor:     %s\n"+
				"Pico:      %d pps / %d bps\n"+
				"Duração:   %s\n"+
				"Incidente: %s\n"+
				"Detalhes:  %s\n",
			ip, vec, ev.PeakPps, ev.PeakBps, dur.String(),
			ev.IncidentID, url,
		)
	}

	return subject, body
}

func vectorLabel(v detect.Vector) string {
	switch v {
	case detect.VectorUDPFlood:
		return "UDP Flood"
	case detect.VectorICMPFlood:
		return "ICMP Flood"
	default:
		return string(v)
	}
}
