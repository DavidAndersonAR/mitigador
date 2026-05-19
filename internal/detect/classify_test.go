package detect_test

import (
	"testing"

	"github.com/mitigador/mitigador/internal/aggregate"
	"github.com/mitigador/mitigador/internal/detect"
)

func TestClassify_DominantUDP(t *testing.T) {
	buckets := []aggregate.Bucket{
		{Pps: 100, Bps: 10000, PpsUDP: 90, BpsUDP: 9000, PpsICMP: 5, PpsOther: 5},
	}
	got := detect.Classify(buckets)
	if got != detect.VectorUDPFlood {
		t.Errorf("expected VectorUDPFlood, got %q", got)
	}
}

func TestClassify_DominantICMP(t *testing.T) {
	buckets := []aggregate.Bucket{
		{Pps: 100, Bps: 10000, PpsICMP: 90, BpsICMP: 9000, PpsUDP: 5, PpsOther: 5},
	}
	got := detect.Classify(buckets)
	if got != detect.VectorICMPFlood {
		t.Errorf("expected VectorICMPFlood, got %q", got)
	}
}

func TestClassify_NoTraffic_ReturnsEmpty(t *testing.T) {
	buckets := []aggregate.Bucket{
		{Pps: 0, Bps: 0},
	}
	got := detect.Classify(buckets)
	if got != "" {
		t.Errorf("expected empty vector for zero traffic, got %q", got)
	}
}

func TestClassify_BalancedReturnsEmpty(t *testing.T) {
	// 50/50 split — neither exceeds 50%.
	buckets := []aggregate.Bucket{
		{Pps: 100, Bps: 10000, PpsUDP: 50, PpsICMP: 50},
	}
	got := detect.Classify(buckets)
	if got != "" {
		t.Errorf("expected empty vector for balanced traffic, got %q", got)
	}
}

func TestClassify_OtherDominant_ReturnsEmpty(t *testing.T) {
	buckets := []aggregate.Bucket{
		{Pps: 100, Bps: 10000, PpsOther: 80, PpsUDP: 10, PpsICMP: 10},
	}
	got := detect.Classify(buckets)
	if got != "" {
		t.Errorf("expected empty vector when Other dominates, got %q", got)
	}
}
