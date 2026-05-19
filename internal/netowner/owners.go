// Package netowner maps IP addresses to a human-readable owner/organization.
//
// This is a starter CIDR table for well-known ASN holders — enough to surface
// "Cloudflare", "Google", "AWS" etc. on a dashboard without pulling in MaxMind
// GeoLite2-ASN. For comprehensive coverage swap this out for an mmdb-backed
// resolver behind the same Lookup() signature.
package netowner

import (
	"net/netip"
)

// Entry pairs a CIDR with the owner string the dashboard renders.
type Entry struct {
	Prefix netip.Prefix
	Owner  string
}

// Default owners table. Order matters only for overlapping ranges — first
// match wins. Keep more specific prefixes before broader ones if they ever
// collide.
//
// Sources: public IANA assignments + the operators' own published ranges
// (e.g. Cloudflare's https://www.cloudflare.com/ips/, Google's
// https://www.gstatic.com/ipranges/cloud.json). Coverage is partial on
// purpose — this is a "say something useful" table, not authoritative.
var owners []Entry

func mustPrefix(s string) netip.Prefix {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		panic("netowner: invalid prefix " + s + ": " + err.Error())
	}
	return p
}

func init() {
	add := func(owner string, cidrs ...string) {
		for _, c := range cidrs {
			owners = append(owners, Entry{Prefix: mustPrefix(c), Owner: owner})
		}
	}

	// Cloudflare
	add("Cloudflare",
		"1.0.0.0/24", "1.1.1.0/24",
		"103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"104.16.0.0/13", "104.24.0.0/14",
		"108.162.192.0/18",
		"131.0.72.0/22",
		"141.101.64.0/18",
		"162.158.0.0/15",
		"172.64.0.0/13",
		"173.245.48.0/20",
		"188.114.96.0/20",
		"190.93.240.0/20",
		"197.234.240.0/22",
		"198.41.128.0/17",
	)

	// Google (incl. YouTube, GCP, public DNS, gstatic, 1e100.net)
	add("Google",
		"8.8.4.0/24", "8.8.8.0/24",
		"34.0.0.0/8",
		"35.184.0.0/13", "35.192.0.0/14", "35.196.0.0/15", "35.198.0.0/16",
		"64.18.0.0/20", "64.233.160.0/19",
		"66.102.0.0/20", "66.249.64.0/19",
		"72.14.192.0/18",
		"74.125.0.0/16",
		"108.177.0.0/17",
		"142.250.0.0/15",
		"172.217.0.0/16",
		"172.253.0.0/16",
		"173.194.0.0/16",
		"209.85.128.0/17",
		"216.58.192.0/19",
		"216.239.32.0/19",
	)

	// Amazon / AWS — broad ranges; an mmdb DB would split by region/service.
	add("Amazon AWS",
		"3.0.0.0/9", "3.128.0.0/9",
		"13.32.0.0/15", "13.224.0.0/14",
		"15.177.0.0/16",
		"18.32.0.0/11", "18.64.0.0/10",
		"34.192.0.0/10", "34.224.0.0/12",
		"35.71.64.0/22", "35.182.0.0/16",
		"52.0.0.0/8",
		"54.144.0.0/12", "54.160.0.0/11", "54.192.0.0/12", "54.208.0.0/13", "54.220.0.0/15",
		"99.78.0.0/18",
		"99.86.0.0/16",
		"205.251.192.0/18",
	)

	// Microsoft (Azure, Bing, MSN, Office)
	add("Microsoft",
		"13.64.0.0/11", "13.96.0.0/13", "13.104.0.0/14",
		"20.0.0.0/8",
		"23.96.0.0/13",
		"40.64.0.0/10",
		"51.4.0.0/15", "51.8.0.0/16", "51.10.0.0/15",
		"52.96.0.0/12", "52.112.0.0/14",
		"104.40.0.0/13", "104.146.0.0/15", "104.208.0.0/13",
		"131.107.0.0/16",
		"137.116.0.0/15",
		"168.61.0.0/16", "168.62.0.0/15",
		"191.232.0.0/13",
	)

	// Apple — they own 17.0.0.0/8 outright.
	add("Apple", "17.0.0.0/8")

	// GitHub
	add("GitHub",
		"140.82.112.0/20",
		"143.55.64.0/20",
		"185.199.108.0/22",
		"192.30.252.0/22",
	)

	// Fastly (Stack Overflow, GitHub Pages caches, etc.)
	add("Fastly",
		"23.235.32.0/20",
		"43.249.72.0/22",
		"103.244.50.0/24", "103.245.222.0/23", "103.245.224.0/24",
		"104.156.80.0/20",
		"151.101.0.0/16",
		"157.52.64.0/18",
		"167.82.0.0/17",
		"172.111.64.0/18",
		"185.31.16.0/22",
		"199.27.72.0/21",
		"199.232.0.0/16",
	)

	// Akamai
	add("Akamai",
		"2.16.0.0/13",
		"23.0.0.0/12",
		"23.32.0.0/11", "23.64.0.0/14",
		"23.192.0.0/11",
		"72.246.0.0/15",
		"95.100.0.0/15",
		"96.16.0.0/15",
		"96.6.0.0/15",
		"104.64.0.0/10",
		"173.222.0.0/15",
		"184.24.0.0/13",
		"184.84.0.0/14",
	)

	// Meta / Facebook / Instagram / WhatsApp
	add("Meta",
		"31.13.24.0/21", "31.13.64.0/18",
		"66.220.144.0/20", "66.220.152.0/21",
		"69.63.176.0/20",
		"69.171.224.0/19",
		"74.119.76.0/22",
		"103.4.96.0/22",
		"129.134.0.0/16",
		"157.240.0.0/16",
		"173.252.64.0/19",
		"179.60.192.0/22",
		"185.60.216.0/22",
		"199.201.64.0/22",
		"204.15.20.0/22",
	)

	// Netflix
	add("Netflix",
		"23.246.0.0/18",
		"37.77.184.0/21",
		"45.57.0.0/17",
		"64.120.128.0/17",
		"66.197.128.0/17",
		"108.175.32.0/20",
		"185.2.220.0/22",
		"185.9.188.0/22",
		"192.173.64.0/18",
		"198.38.96.0/19",
		"198.45.48.0/20",
	)

	// Quad9
	add("Quad9", "9.9.9.0/24", "149.112.112.0/24")
	// OpenDNS / Cisco
	add("OpenDNS", "208.67.220.0/24", "208.67.222.0/24")

	// X / Twitter
	add("X (Twitter)", "104.244.42.0/24", "199.59.148.0/22")

	// Twitch (Amazon-owned but distinct branding)
	add("Twitch", "192.108.239.0/24", "199.9.248.0/22")
}

// cidrLookup returns the owner string for ip from the hand-curated CIDR
// table, or "" if no entry matches. It is the fallback used by Resolver
// when the mmdb-backed lookup has no answer.
//
// Kept linear-scan because the table is ~150 entries; if it grows past a
// few hundred swap to a sorted-by-prefix-length search.
func cidrLookup(ip netip.Addr) string {
	if !ip.IsValid() {
		return ""
	}
	for _, e := range owners {
		if e.Prefix.Contains(ip) {
			return e.Owner
		}
	}
	return ""
}
