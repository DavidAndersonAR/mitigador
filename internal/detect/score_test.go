package detect_test

import (
	"testing"

	"github.com/mitigador/mitigador/internal/detect"
)

func TestConfidence_AtThreshold_Is0(t *testing.T) {
	th := detect.Threshold{PPS: 1000, BPS: 100_000, MinWindowSec: 5}
	// Exactly at threshold — score should be 0 (or very close due to floating point).
	got := detect.Confidence(1000, 100_000, th, 5)
	if got != 0.0 {
		t.Errorf("expected 0 at threshold, got %f", got)
	}
}

func TestConfidence_DoubleThreshold_PositiveAndBounded(t *testing.T) {
	th := detect.Threshold{PPS: 1000, BPS: 100_000, MinWindowSec: 5}
	// Double the threshold — score must be > 0 and <= 1.
	got := detect.Confidence(2000, 200_000, th, 10)
	if got <= 0 {
		t.Errorf("expected positive score at 2× threshold, got %f", got)
	}
	if got > 1.0 {
		t.Errorf("score exceeds 1.0: %f", got)
	}
}

func TestConfidence_ClampedTo1(t *testing.T) {
	th := detect.Threshold{PPS: 1000, BPS: 100_000, MinWindowSec: 5}
	// Extremely high traffic — score must be clamped to 1.
	got := detect.Confidence(1_000_000, 1_000_000_000, th, 3600)
	if got > 1.0 {
		t.Errorf("score should be clamped to 1.0, got %f", got)
	}
	if got < 0.9 {
		t.Errorf("expected near-max score, got %f", got)
	}
}
