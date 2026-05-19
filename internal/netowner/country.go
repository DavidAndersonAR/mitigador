package netowner

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/oschwald/maxminddb-golang"
)

// countryRecord covers both MaxMind GeoLite2-Country and db-ip Country-Lite
// schemas. db-ip-lite uses a flat `country.iso_code` / `country.names.en`
// layout that matches MaxMind exactly — the same struct decodes both.
type countryRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Continent struct {
		Code  string            `maxminddb:"code"`
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"continent"`
}

// CountryMMDB wraps an open maxminddb reader for Country lookups.
type CountryMMDB struct {
	db   *maxminddb.Reader
	path string
}

// OpenCountryMMDB opens a GeoLite2-Country (or db-ip-country-lite) .mmdb file.
func OpenCountryMMDB(path string) (*CountryMMDB, error) {
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("netowner: open country mmdb %q: %w", path, err)
	}
	return &CountryMMDB{db: r, path: path}, nil
}

// Close releases the mmap. Safe on a nil receiver.
func (c *CountryMMDB) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Path returns the source file path.
func (c *CountryMMDB) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

// Country returns (iso_code, country_name, continent_code) for ip.
// Empty strings on miss / invalid input / nil receiver.
func (c *CountryMMDB) Country(ip netip.Addr) (iso, name, continent string) {
	if c == nil || c.db == nil || !ip.IsValid() {
		return "", "", ""
	}
	var rec countryRecord
	if err := c.db.Lookup(net.IP(ip.AsSlice()), &rec); err != nil {
		return "", "", ""
	}
	iso = rec.Country.ISOCode
	if rec.Country.Names != nil {
		name = rec.Country.Names["en"]
	}
	continent = rec.Continent.Code
	return
}
