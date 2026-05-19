package subscriber

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// RouterConfig describes one Mikrotik device to poll over SSH.
//
// URL field is reused as host[:port] for backwards compatibility with the
// existing DB schema. Any leading scheme (`ssh://`, `https://`, `http://`)
// is stripped; missing port defaults to 22.
type RouterConfig struct {
	Name      string
	URL       string        // host or host:port (any scheme prefix is stripped)
	Username  string        // SSH user (on Mikrotik: the same admin/api user)
	Password  string        // SSH password
	VerifyTLS bool          // ignored on SSH transport; retained for schema compat
	Timeout   time.Duration // per-request timeout; defaults to 5s
}

// RouterProvider returns the routers to poll on each refresh cycle.
type RouterProvider interface {
	Routers(ctx context.Context) ([]RouterConfig, error)
}

// StaticRouters is a tiny RouterProvider that returns the same list every time.
type StaticRouters []RouterConfig

func (s StaticRouters) Routers(context.Context) ([]RouterConfig, error) { return s, nil }

// PollerConfig holds the dynamic-subscriber poller settings.
type PollerConfig struct {
	Provider     RouterProvider
	PollInterval time.Duration // refresh cadence; defaults to 30s if zero
}

// Poller refreshes the Store with active PPPoE + DHCP sessions from
// every configured router.
type Poller struct {
	cfg   PollerConfig
	store *Store
}

// NewPoller builds a poller.
func NewPoller(cfg PollerConfig, store *Store) *Poller {
	return &Poller{cfg: cfg, store: store}
}

// Run polls every PollInterval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	interval := p.cfg.PollInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	p.refresh(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			p.refresh(ctx)
		}
	}
}

func (p *Poller) refresh(ctx context.Context) {
	next := make(map[netip.Addr]*Subscriber)
	now := time.Now()
	routers, err := p.cfg.Provider.Routers(ctx)
	if err != nil {
		slog.Warn("subscriber: provider failed", "err", err.Error())
		return
	}
	for _, r := range routers {
		if r.Name == "" || r.URL == "" {
			continue
		}
		subs, err := p.pollRouter(ctx, r)
		if err != nil {
			slog.Warn("subscriber: poll failed", "router", r.Name, "err", err.Error())
			continue
		}
		for _, sub := range subs {
			sub.LastSeen = now
			next[sub.IP] = sub
		}
	}
	p.store.Replace(next)
	slog.Debug("subscriber: refreshed", "total", len(next), "routers", len(routers))
}

// TestConnection authenticates and runs `/system identity print` so the UI
// can probe credentials before saving them.
func TestConnection(ctx context.Context, r RouterConfig) (string, error) {
	out, err := runSSH(ctx, r, "/system identity print")
	if err != nil {
		return "", err
	}
	// Output format: "                name: <NAME>\n"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name:")), nil
		}
	}
	return "router-reachable", nil
}

func (p *Poller) pollRouter(ctx context.Context, r RouterConfig) ([]*Subscriber, error) {
	var out []*Subscriber
	if subs, err := p.fetchPPPoE(ctx, r); err == nil {
		out = append(out, subs...)
	} else if !isNotSupported(err) {
		return nil, fmt.Errorf("ppp: %w", err)
	}
	if subs, err := p.fetchDHCPLeases(ctx, r); err == nil {
		out = append(out, subs...)
	} else if !isNotSupported(err) {
		return out, fmt.Errorf("dhcp: %w", err)
	}
	return out, nil
}

func (p *Poller) fetchPPPoE(ctx context.Context, r RouterConfig) ([]*Subscriber, error) {
	out, err := runSSH(ctx, r, "/ppp active print as-value")
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var subs []*Subscriber
	for _, rec := range parseAsValue(out) {
		addr := rec["address"]
		if addr == "" {
			continue
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(addr))
		if err != nil || !ip.IsValid() {
			continue
		}
		subs = append(subs, &Subscriber{
			IP:             ip.Unmap(),
			Username:       firstNonEmpty(rec["name"], rec["user"]),
			Service:        firstNonEmpty(rec["service"], "pppoe"),
			Router:         r.Name,
			Comment:        firstNonEmpty(rec["comment"], rec["caller-id"]),
			ConnectedSince: subtractUptime(now, rec["uptime"]),
		})
	}
	return subs, nil
}

func (p *Poller) fetchDHCPLeases(ctx context.Context, r RouterConfig) ([]*Subscriber, error) {
	out, err := runSSH(ctx, r, "/ip dhcp-server lease print where status=bound as-value")
	if err != nil {
		return nil, err
	}
	var subs []*Subscriber
	for _, rec := range parseAsValue(out) {
		addr := rec["address"]
		if addr == "" {
			continue
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(addr))
		if err != nil || !ip.IsValid() {
			continue
		}
		name := firstNonEmpty(rec["host-name"], rec["mac-address"])
		subs = append(subs, &Subscriber{
			IP:       ip.Unmap(),
			Username: name,
			Service:  "dhcp",
			Router:   r.Name,
			Comment:  firstNonEmpty(rec["comment"], rec["mac-address"]),
		})
	}
	return subs, nil
}

// ─── SSH transport ──────────────────────────────────────────────────────

// runSSH connects, runs a single Mikrotik CLI command, and returns the
// combined stdout output.
func runSSH(ctx context.Context, r RouterConfig, cmd string) (string, error) {
	addr := normalizeHost(r.URL)
	if addr == "" {
		return "", errors.New("ssh: empty host")
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	cfg := &ssh.ClientConfig{
		User:            r.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(r.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         timeout,
	}

	type dialResult struct {
		client *ssh.Client
		err    error
	}
	done := make(chan dialResult, 1)
	go func() {
		c, e := ssh.Dial("tcp", addr, cfg)
		done <- dialResult{client: c, err: e}
	}()

	var client *ssh.Client
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-done:
		if res.err != nil {
			return "", fmt.Errorf("ssh dial %s: %w", addr, res.err)
		}
		client = res.client
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh session: %w", err)
	}
	defer sess.Close()

	// Use CombinedOutput with our own timeout enforcement — most Mikrotik
	// CLI commands return in <1s, so 5s is generous.
	type outResult struct {
		out []byte
		err error
	}
	oc := make(chan outResult, 1)
	go func() {
		b, e := sess.CombinedOutput(cmd)
		oc <- outResult{out: b, err: e}
	}()

	select {
	case <-ctx.Done():
		_ = sess.Close()
		return "", ctx.Err()
	case res := <-oc:
		if res.err != nil {
			// Distinguish "command not supported" so callers can degrade.
			snippet := string(res.out)
			if strings.Contains(snippet, "no such command") || strings.Contains(snippet, "expected end of command") {
				return "", errNotSupported
			}
			return "", fmt.Errorf("ssh exec: %w (stderr=%s)", res.err, strings.TrimSpace(snippet))
		}
		return string(res.out), nil
	}
}

// normalizeHost accepts any of:
//
//	"45.6.188.40"            → "45.6.188.40:22"
//	"45.6.188.40:2222"       → "45.6.188.40:2222"
//	"https://45.6.188.40"    → "45.6.188.40:22"
//	"ssh://router.local:22"  → "router.local:22"
func normalizeHost(s string) string {
	s = strings.TrimSpace(s)
	for _, scheme := range []string{"ssh://", "https://", "http://"} {
		s = strings.TrimPrefix(s, scheme)
	}
	s = strings.TrimRight(s, "/")
	if s == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		s = net.JoinHostPort(s, "22")
	}
	return s
}

// ─── as-value parsing ───────────────────────────────────────────────────

// parseAsValue parses Mikrotik's `... as-value` output:
//
//	.id=*1;name=joao;service=pppoe;address=100.64.17.10
//	.id=*2;name=maria;service=pppoe;address=100.64.17.11
//
// Some RouterOS versions emit one entry per line, others put them all on
// one line separated by `;;;`. We handle both by splitting on newline AND
// the triple-semicolon marker.
func parseAsValue(s string) []map[string]string {
	var out []map[string]string
	// Triple semicolon = record separator in some versions.
	for _, chunk := range strings.Split(s, ";;;") {
		scanner := bufio.NewScanner(strings.NewReader(chunk))
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			rec := parseAsValueLine(line)
			if len(rec) > 0 {
				out = append(out, rec)
			}
		}
	}
	return out
}

// parseAsValueLine splits one "k=v;k=v" line into a map.
func parseAsValueLine(line string) map[string]string {
	rec := map[string]string{}
	for _, kv := range strings.Split(line, ";") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		eq := strings.Index(kv, "=")
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(kv[:eq])
		v := strings.TrimSpace(kv[eq+1:])
		v = strings.Trim(v, `"`)
		rec[k] = v
	}
	return rec
}

// ─── helpers ────────────────────────────────────────────────────────────

var errNotSupported = errors.New("command not supported on this router")

func isNotSupported(err error) bool { return errors.Is(err, errNotSupported) }

func firstNonEmpty(a, b string) string {
	a = strings.TrimSpace(a)
	if a != "" {
		return a
	}
	return strings.TrimSpace(b)
}

// subtractUptime parses Mikrotik's "1w2d3h4m5s" format and returns the
// approximate connection-start time.
func subtractUptime(now time.Time, uptime string) time.Time {
	if uptime == "" {
		return time.Time{}
	}
	var d time.Duration
	var num int64
	for _, c := range uptime {
		switch {
		case c >= '0' && c <= '9':
			num = num*10 + int64(c-'0')
		case c == 'w':
			d += time.Duration(num) * 7 * 24 * time.Hour
			num = 0
		case c == 'd':
			d += time.Duration(num) * 24 * time.Hour
			num = 0
		case c == 'h':
			d += time.Duration(num) * time.Hour
			num = 0
		case c == 'm':
			d += time.Duration(num) * time.Minute
			num = 0
		case c == 's':
			d += time.Duration(num) * time.Second
			num = 0
		default:
			return time.Time{}
		}
	}
	if d == 0 {
		return time.Time{}
	}
	return now.Add(-d)
}

// Compile-time guard: keep io import path for future use of io.Reader.
var _ = io.EOF
