// flowgen sends synthetic NetFlow v9 datagrams to a configurable UDP target.
//
// Dev/test only — NOT shipped in releases (excluded by goreleaser config).
//
// Usage:
//
//	flowgen --target 127.0.0.1:2055 --src 10.0.0.1 --dst 192.0.2.10 \
//	        --pps 1000 --bytes 1500 --duration 30s --proto 17
//
// The generator first sends a NetFlow v9 template FlowSet (required by the
// receiver to parse subsequent data records) then sends one data FlowSet per
// --interval tick until --duration elapses.
//
// To exercise the full detection pipeline:
//  1. Add the --src IP to the exporters table (mitigador config sync).
//  2. Add the --dst subnet to a hostgroup with a threshold below --pps / --bps.
//  3. Run mitigador serve in one terminal.
//  4. Run flowgen in another terminal.
//  5. Watch the dashboard at http://localhost:8080 for the incident.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/netip"
	"time"
)

func main() {
	var (
		target      = flag.String("target", "127.0.0.1:2055", "UDP target host:port (mitigador NetFlow listener)")
		srcIPStr    = flag.String("src", "10.0.0.1", "exporter source IP (must be in the exporters inventory)")
		dstIPStr    = flag.String("dst", "192.0.2.10", "victim host IP (must be in a configured hostgroup)")
		pps         = flag.Int("pps", 1000, "synthetic packets per second reported in each emitted flow record")
		bytesPerPkt = flag.Int("bytes", 1500, "bytes per packet (used to compute IN_BYTES = pps * bytes)")
		duration    = flag.Duration("duration", 30*time.Second, "total send duration before flowgen exits")
		proto       = flag.Int("proto", 17, "L4 protocol number: 17=UDP, 1=ICMP, 6=TCP")
		interval    = flag.Duration("interval", time.Second, "time between datagrams (default 1s)")
	)
	flag.Parse()

	src, err := netip.ParseAddr(*srcIPStr)
	if err != nil {
		log.Fatalf("flowgen: invalid --src IP %q: %v", *srcIPStr, err)
	}
	if !src.Is4() {
		log.Fatalf("flowgen: --src must be an IPv4 address, got %q", *srcIPStr)
	}

	dst, err := netip.ParseAddr(*dstIPStr)
	if err != nil {
		log.Fatalf("flowgen: invalid --dst IP %q: %v", *dstIPStr, err)
	}
	if !dst.Is4() {
		log.Fatalf("flowgen: --dst must be an IPv4 address, got %q", *dstIPStr)
	}

	if *pps < 1 {
		log.Fatalf("flowgen: --pps must be >= 1")
	}
	if *bytesPerPkt < 1 {
		log.Fatalf("flowgen: --bytes must be >= 1")
	}
	if *proto < 0 || *proto > 255 {
		log.Fatalf("flowgen: --proto must be 0–255")
	}

	// Dial UDP — try binding from the source IP so the exporter source-IP check
	// on the mitigador side sees the correct IP. Fall back to unbound on error.
	conn, err := net.DialUDP("udp", &net.UDPAddr{IP: net.ParseIP(*srcIPStr)}, mustResolveUDP(*target))
	if err != nil {
		// Fallback: let the OS pick a source address (useful in unit tests
		// where binding to an arbitrary IP is not permitted).
		log.Printf("flowgen: could not bind to src %s, using unbound socket: %v", *srcIPStr, err)
		c, err2 := net.Dial("udp", *target)
		if err2 != nil {
			log.Fatalf("flowgen: dial %s: %v", *target, err2)
		}
		defer c.Close()
		runWithConn(c, src, dst, uint8(*proto), *pps, *bytesPerPkt, *duration, *interval)
		return
	}
	defer conn.Close()
	runWithConn(conn, src, dst, uint8(*proto), *pps, *bytesPerPkt, *duration, *interval)
}

// runWithConn sends the template then ticks until duration elapses.
func runWithConn(conn net.Conn, src, dst netip.Addr, proto uint8, pps, bytesPerPkt int, duration, interval time.Duration) {
	// Send template FlowSet first — receiver needs it to decode data records.
	tmpl := buildTemplate()
	if _, err := conn.Write(tmpl); err != nil {
		log.Fatalf("flowgen: write template: %v", err)
	}

	end := time.Now().Add(duration)
	tick := time.NewTicker(interval)
	defer tick.Stop()

	var seq uint32 = 1
	pkts := uint64(pps)
	bytes := uint64(pps) * uint64(bytesPerPkt)

	fmt.Printf("flowgen: sending to %s | src=%s dst=%s proto=%d pps=%d bps=%d duration=%s\n",
		conn.RemoteAddr(), src, dst, proto, pps, bytes*8, duration)

	for now := time.Now(); now.Before(end); now = <-tick.C {
		dgram := buildDataRecord(seq, src, dst, proto, pkts, bytes)
		if _, err := conn.Write(dgram); err != nil {
			log.Printf("flowgen: write seq=%d: %v", seq, err)
		}
		seq++
	}
	fmt.Printf("flowgen: done (sent %d datagrams)\n", seq-1)
}

// mustResolveUDP resolves a host:port string; exits on failure.
func mustResolveUDP(addr string) *net.UDPAddr {
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Fatalf("flowgen: resolve %q: %v", addr, err)
	}
	return a
}
