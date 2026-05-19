package netowner

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/oschwald/maxminddb-golang"
)

// asnRecord matches the schema used by both MaxMind GeoLite2-ASN and
// db-ip ASN-Lite mmdb files.
type asnRecord struct {
	AutonomousSystemNumber       uint32 `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string `maxminddb:"autonomous_system_organization"`
}

// MMDB is a thin wrapper around an open maxminddb reader for ASN lookups.
// The reader is safe for concurrent use; no locking needed on top.
type MMDB struct {
	db   *maxminddb.Reader
	path string
}

// OpenMMDB opens the given .mmdb file for ASN lookups.
// Returns an error if the file is missing or has an incompatible schema
// (i.e. lacks the autonomous_system_* fields).
func OpenMMDB(path string) (*MMDB, error) {
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("netowner: open mmdb %q: %w", path, err)
	}
	return &MMDB{db: r, path: path}, nil
}

// Close releases the mmap. Safe to call on a nil receiver.
func (m *MMDB) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

// Path returns the source file path. Useful for logging/metrics.
func (m *MMDB) Path() string {
	if m == nil {
		return ""
	}
	return m.path
}

// Lookup returns the AS number and organization name for ip.
// Returns (0, "") on miss, invalid input, or a nil receiver.
func (m *MMDB) Lookup(ip netip.Addr) (uint32, string) {
	if m == nil || m.db == nil || !ip.IsValid() {
		return 0, ""
	}
	var rec asnRecord
	if err := m.db.Lookup(net.IP(ip.AsSlice()), &rec); err != nil {
		return 0, ""
	}
	return rec.AutonomousSystemNumber, rec.AutonomousSystemOrganization
}

// Resolver is the public lookup surface used by the API layer.
// Construct with New; any field may be nil and lookups still work
// (ASN degrades to the CIDR fallback; Country returns empty).
type Resolver struct {
	mmdb    *MMDB
	country *CountryMMDB
}

// New returns a resolver that uses mmdb (if non-nil) and falls back to the
// hand-curated CIDR table for IPs the mmdb does not cover. The country mmdb
// is optional — pass nil to disable country enrichment.
func New(mmdb *MMDB, country *CountryMMDB) *Resolver {
	return &Resolver{mmdb: mmdb, country: country}
}

// CountryISO returns the ISO-3166 alpha-2 code (e.g. "BR", "US") for ip,
// or "" if no Country mmdb is loaded / no match.
func (r *Resolver) CountryISO(ip netip.Addr) string {
	if r == nil || r.country == nil {
		return ""
	}
	iso, _, _ := r.country.Country(ip)
	return iso
}

// CountryName returns the English country name for ip ("United States"),
// or "" if unavailable.
func (r *Resolver) CountryName(ip netip.Addr) string {
	if r == nil || r.country == nil {
		return ""
	}
	_, name, _ := r.country.Country(ip)
	return name
}

// Lookup returns the organization name for ip, or "" if neither source
// has a record. The string is normalized for compact UI display — long
// corporate suffixes are truncated and trimmed.
func (r *Resolver) Lookup(ip netip.Addr) string {
	if r != nil && r.mmdb != nil {
		if _, org := r.mmdb.Lookup(ip); org != "" {
			return shortenOrg(org)
		}
	}
	return cidrLookup(ip)
}

// LookupDetailed returns the full "AS{number} {org}" string when the mmdb
// has a record; falls back to the CIDR label. Used for tooltips where the
// extra context is welcome.
func (r *Resolver) LookupDetailed(ip netip.Addr) string {
	if r != nil && r.mmdb != nil {
		if asn, org := r.mmdb.Lookup(ip); org != "" {
			return fmt.Sprintf("AS%d %s", asn, org)
		}
	}
	return cidrLookup(ip)
}

// shortenOrg trims noisy corporate suffixes ("Ltd.", "Inc.", "S.A.") and
// caps long names so the dashboard chip stays a single line. The full name
// is still available via LookupDetailed for tooltips.
func shortenOrg(org string) string {
	const maxLen = 24
	// Strip everything after a comma — usually subsidiary / country tail.
	if i := indexByte(org, ','); i >= 0 {
		org = org[:i]
	}
	// Drop common corporate suffixes.
	for _, suf := range []string{
		" S.A.", " S/A", " S.p.A.", " Ltd.", " Ltd", " LLC", " L.L.C.",
		" Inc.", " Inc", " Corporation", " Corp.", " Corp", " GmbH",
		" plc", " AG", " Limited", " Company", " Co.", " Co",
		" Pvt", " Pty",
	} {
		if hasSuffix(org, suf) {
			org = org[:len(org)-len(suf)]
		}
	}
	org = trimSpace(org)
	if len(org) > maxLen {
		// Cut at a word boundary if possible.
		cut := maxLen
		for i := maxLen; i > maxLen-8 && i > 0; i-- {
			if org[i] == ' ' {
				cut = i
				break
			}
		}
		org = trimSpace(org[:cut]) + "…"
	}
	return org
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
