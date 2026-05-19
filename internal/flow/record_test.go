package flow_test

import (
	"testing"

	"github.com/mitigador/mitigador/internal/flow"
)

func TestProtoConstants(t *testing.T) {
	if flow.ProtoICMP != 1 {
		t.Errorf("ICMP = %d, want 1", flow.ProtoICMP)
	}
	if flow.ProtoTCP != 6 {
		t.Errorf("TCP = %d, want 6", flow.ProtoTCP)
	}
	if flow.ProtoUDP != 17 {
		t.Errorf("UDP = %d, want 17", flow.ProtoUDP)
	}
	if flow.ProtoOther != 0 {
		t.Errorf("ProtoOther = %d, want 0", flow.ProtoOther)
	}
}
