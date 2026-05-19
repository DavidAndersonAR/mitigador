// Package version exposes build-time information set via -ldflags by goreleaser.
package version

// These three vars are overridden at build time via `-ldflags "-X github.com/mitigador/mitigador/internal/version.Version=v0.1.0 -X github.com/mitigador/mitigador/internal/version.Commit=abc123 -X github.com/mitigador/mitigador/internal/version.Date=2026-05-18"`.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a one-line human-friendly representation.
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
