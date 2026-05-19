// Package telegram provides Telegram alert formatting and sending for Mitigador.
package telegram

import (
	"fmt"
	"strings"
	"time"

	"github.com/mitigador/mitigador/internal/detect"
)

// mdv2Escape escapes MarkdownV2 special characters per the Telegram Bot API spec.
// All dynamic string values (IPs, hostgroup names, URLs) must be escaped before
// interpolation into a MarkdownV2 message.
func mdv2Escape(s string) string {
	special := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	for _, c := range special {
		s = strings.ReplaceAll(s, c, `\`+c)
	}
	return s
}

func humanBits(bps uint64) string {
	const k = 1000.0
	v := float64(bps)
	units := []string{"bps", "kbps", "Mbps", "Gbps", "Tbps"}
	for _, u := range units {
		if v < k {
			return fmt.Sprintf("%.1f %s", v, u)
		}
		v /= k
	}
	return fmt.Sprintf("%.1f Pbps", v)
}

func humanPackets(pps uint64) string {
	if pps < 1000 {
		return fmt.Sprintf("%d pps", pps)
	}
	if pps < 1_000_000 {
		return fmt.Sprintf("%.1f kpps", float64(pps)/1000)
	}
	return fmt.Sprintf("%.1f Mpps", float64(pps)/1_000_000)
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

// Format returns the pt-BR MarkdownV2-escaped Telegram message body for ev.
// Returns empty string for unknown states (caller should skip empty results).
func Format(ev detect.AttackEvent, appBaseURL string) string {
	ip := mdv2Escape(ev.HostIP.String())
	vec := mdv2Escape(vectorLabel(ev.Vector))
	hg := mdv2Escape(ev.Hostgroup)
	url := mdv2Escape(fmt.Sprintf("%s/incidents/%s", strings.TrimRight(appBaseURL, "/"), ev.IncidentID))

	// Use full IncidentID for the link; show shortened label for readability.
	// IncidentID is a ULID (26 chars) — safe to show in full.
	incShort := ev.IncidentID
	if len(incShort) > 12 {
		incShort = incShort[:12]
	}
	inc := mdv2Escape(incShort)

	switch ev.State {
	case detect.StateStarted:
		return fmt.Sprintf(
			"🚨 *Ataque detectado*\n"+
				"IP alvo: `%s`\n"+
				"Vetor: *%s*\n"+
				"Taxa: %s / %s\n"+
				"Hostgroup: %s\n"+
				"Incidente: `%s`\n"+
				"Detalhes: %s",
			ip, vec,
			mdv2Escape(humanPackets(ev.Pps)),
			mdv2Escape(humanBits(ev.Bps)),
			hg, inc, url,
		)

	case detect.StateUpdated:
		return fmt.Sprintf(
			"📈 *Ataque ainda em curso*\n"+
				"IP alvo: `%s`\n"+
				"Vetor: *%s*\n"+
				"Pico: %s / %s\n"+
				"Incidente: `%s`\n"+
				"Detalhes: %s",
			ip, vec,
			mdv2Escape(humanPackets(ev.PeakPps)),
			mdv2Escape(humanBits(ev.PeakBps)),
			inc, url,
		)

	case detect.StateEnded:
		dur := ev.EndedAt.Sub(ev.StartedAt).Round(time.Second)
		return fmt.Sprintf(
			"✅ *Ataque encerrado*\n"+
				"IP alvo: `%s`\n"+
				"Vetor: *%s*\n"+
				"Pico: %s / %s\n"+
				"Duração: %s\n"+
				"Incidente: `%s`\n"+
				"Detalhes: %s",
			ip, vec,
			mdv2Escape(humanPackets(ev.PeakPps)),
			mdv2Escape(humanBits(ev.PeakBps)),
			mdv2Escape(dur.String()),
			inc, url,
		)
	}

	return ""
}
