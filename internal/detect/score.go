package detect

import "math"

// Confidence combines pps_ratio, bps_ratio, and duration_factor into a 0..1 score.
// At threshold (ratio=1) the score is 0. Score saturates toward 1 as the breach
// gets larger or longer. The duration factor only contributes when at least one
// of pps or bps is above threshold (ratio > 1) — i.e., during an active violation.
//
// Weights: pps 40%, bps 40%, duration 20%.
func Confidence(avgPps, avgBps uint64, t Threshold, durSec int) float64 {
	ppsRatio := float64(avgPps) / float64(maxU64(t.PPS, 1))
	bpsRatio := float64(avgBps) / float64(maxU64(t.BPS, 1))

	// Duration factor only applies above threshold (violation window).
	var durFactor float64
	if (ppsRatio > 1 || bpsRatio > 1) && t.MinWindowSec > 0 {
		durFactor = math.Min(float64(durSec)/float64(t.MinWindowSec), 1.0)
	}

	score := 0.4*sat(ppsRatio) + 0.4*sat(bpsRatio) + 0.2*durFactor
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

// maxU64 returns the larger of a and b.
func maxU64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// sat maps a ratio value to a [0,1) saturation via tanh.
// Returns 0 for x <= 1 (at or below threshold).
// Returns tanh(x-1) for x > 1 (above threshold), which saturates toward 1.
func sat(x float64) float64 {
	if x <= 1 {
		return 0
	}
	return math.Tanh(x - 1)
}
