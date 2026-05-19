// Package main — flowgen NetFlow v9 wire format helpers.
//
// NetFlow v9 frame layout (RFC 3954):
//
//	NetFlow v9 Packet:
//	  [Header 20 bytes]
//	  [FlowSet: Template FlowSet (FlowSetID=0)]
//	  [FlowSet: Data FlowSet (FlowSetID=template_id)]
//
// Template FlowSet structure:
//
//	u16 FlowSetID = 0      (template)
//	u16 Length             (includes header)
//	u16 Template ID        (must be ≥ 256)
//	u16 Field Count        (number of field type+length pairs)
//	  per field:
//	    u16 Field Type     (IANA field number)
//	    u16 Field Length   (bytes)
//
// Data FlowSet structure:
//
//	u16 FlowSetID = templateID
//	u16 Length              (includes header)
//	  per record (fixed width, all fields concatenated):
//	    [field values in template order]
//
// Fields used in this generator (5 fields, matching mitigador's ingest decoder):
//
//	Field Type 8  = IPV4_SRC_ADDR  (4 bytes)
//	Field Type 12 = IPV4_DST_ADDR  (4 bytes)
//	Field Type 4  = PROTOCOL       (1 byte)
//	Field Type 1  = IN_BYTES       (8 bytes)   — using 64-bit to avoid overflow
//	Field Type 2  = IN_PKTS        (8 bytes)   — using 64-bit

package main

import (
	"encoding/binary"
	"net/netip"
)

const (
	templateID = 256 // must be ≥ 256 per RFC 3954

	// fieldCount is the number of (type, length) pairs in the template.
	fieldCount = 5

	// recordSize is the fixed byte length of one data record (sum of field lengths).
	// 4 + 4 + 1 + 8 + 8 = 25 bytes per record.
	recordSize = 25
)

// field describes one NetFlow v9 field.
type field struct {
	fieldType   uint16
	fieldLength uint16
}

// templateFields defines the 5 fields our generator emits.
// This must match exactly what the receiver decodes — ordering matters.
var templateFields = []field{
	{8, 4},  // IPV4_SRC_ADDR  — 4 bytes
	{12, 4}, // IPV4_DST_ADDR  — 4 bytes
	{4, 1},  // PROTOCOL       — 1 byte
	{1, 8},  // IN_BYTES       — 8 bytes (64-bit)
	{2, 8},  // IN_PKTS        — 8 bytes (64-bit)
}

// buildTemplate returns a complete NetFlow v9 packet containing only a template
// FlowSet. This must be sent BEFORE any data records so the receiver can parse
// subsequent data FlowSets.
func buildTemplate() []byte {
	// Template FlowSet payload = template ID (2) + field count (2) + fields (fieldCount * 4).
	tmplPayload := 2 + 2 + fieldCount*4 // = 24 bytes

	// Template FlowSet = FlowSetID (2) + Length (2) + payload.
	tmplFlowSetLen := 4 + tmplPayload // = 28 bytes

	pkt := make([]byte, 0, 20+tmplFlowSetLen)

	// --- NetFlow v9 Header (20 bytes) ---
	pkt = appendU16(pkt, 9)           // Version = 9
	pkt = appendU16(pkt, 1)           // Count = 1 (one FlowSet — the template counts as 1)
	pkt = appendU32(pkt, 0)           // SysUptime (ms) — dummy
	pkt = appendU32(pkt, 0)           // UnixSecs — dummy (receiver uses arrival time)
	pkt = appendU32(pkt, 0)           // PackageSequence
	pkt = appendU32(pkt, 0)           // SourceID

	// --- Template FlowSet ---
	pkt = appendU16(pkt, 0)                       // FlowSetID = 0 (template)
	pkt = appendU16(pkt, uint16(tmplFlowSetLen))  // Length of this FlowSet
	pkt = appendU16(pkt, templateID)              // Template ID
	pkt = appendU16(pkt, fieldCount)              // Field Count

	for _, f := range templateFields {
		pkt = appendU16(pkt, f.fieldType)
		pkt = appendU16(pkt, f.fieldLength)
	}

	return pkt
}

// buildDataRecord returns a complete NetFlow v9 packet containing a single data
// FlowSet with one record. seq is the PacketSequence counter; it should increment
// with every datagram to allow the receiver to detect losses.
func buildDataRecord(seq uint32, src, dst netip.Addr, proto uint8, pkts, bytes uint64) []byte {
	// Data FlowSet = FlowSetID (2) + Length (2) + record bytes.
	dataFlowSetLen := 4 + recordSize // = 29 bytes

	pkt := make([]byte, 0, 20+dataFlowSetLen)

	// --- NetFlow v9 Header (20 bytes) ---
	pkt = appendU16(pkt, 9)    // Version = 9
	pkt = appendU16(pkt, 1)    // Count = 1 (one FlowSet)
	pkt = appendU32(pkt, 0)    // SysUptime (ms)
	pkt = appendU32(pkt, 0)    // UnixSecs
	pkt = appendU32(pkt, seq)  // PacketSequence
	pkt = appendU32(pkt, 0)    // SourceID

	// --- Data FlowSet ---
	pkt = appendU16(pkt, templateID)              // FlowSetID = templateID
	pkt = appendU16(pkt, uint16(dataFlowSetLen))  // Length of this FlowSet

	// One data record — fields in template order.
	src4 := src.As4()
	dst4 := dst.As4()
	pkt = append(pkt, src4[:]...)  // IPV4_SRC_ADDR (4 bytes)
	pkt = append(pkt, dst4[:]...)  // IPV4_DST_ADDR (4 bytes)
	pkt = append(pkt, proto)       // PROTOCOL (1 byte)
	pkt = appendU64(pkt, bytes)    // IN_BYTES (8 bytes)
	pkt = appendU64(pkt, pkts)     // IN_PKTS (8 bytes)

	return pkt
}

// appendU16 appends a big-endian uint16 to b (RFC 3954 wire format).
func appendU16(b []byte, v uint16) []byte {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	return append(b, buf[:]...)
}

// appendU32 appends a big-endian uint32 to b.
func appendU32(b []byte, v uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	return append(b, buf[:]...)
}

// appendU64 appends a big-endian uint64 to b.
func appendU64(b []byte, v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return append(b, buf[:]...)
}
