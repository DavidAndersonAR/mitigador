package subscriber

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// RouterConfig describes one Mikrotik device to poll.
type RouterConfig struct {
	Name      string        // friendly name surfaced on subscriber chips
	URL       string        // base URL, e.g. https://10.0.0.1
	Username  string        // REST API user (admin or read-only)
	Password  string        // REST API password
	VerifyTLS bool          // false = accept self-signed certs (Mikrotik default)
	Timeout   time.Duration // per-request HTTP timeout; defaults to 5s
}

// PollerConfig holds the dynamic-subscriber poller settings.
type PollerConfig struct {
	Routers      []RouterConfig
	PollInterval time.Duration // refresh cadence; defaults to 30s if zero
}

// Poller refreshes the Store with active PPPoE + DHCP sessions from
// every configured router.
type Poller struct {
	cfg    PollerConfig
	store  *Store
	client *http.Client
}

// NewPoller builds a poller. The HTTP client is shared across routers; each
// request still respects the per-router timeout / TLS verification setting.
func NewPoller(cfg PollerConfig, store *Store) *Poller {
	return &Poller{cfg: cfg, store: store, client: &http.Client{}}
}

// Run polls every PollInterval until ctx is cancelled. Returns ctx.Err() on
// shutdown so it composes cleanly inside an errgroup.
func (p *Poller) Run(ctx context.Context) error {
	interval := p.cfg.PollInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	// Initial refresh before the first tick so the dashboard does not stay
	// empty for `interval` seconds after startup.
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

// refresh hits every router once and atomically replaces the store snapshot
// with the union of results. A router failure does not invalidate entries
// learned from another router in the same cycle.
func (p *Poller) refresh(ctx context.Context) {
	next := make(map[netip.Addr]*Subscriber)
	now := time.Now()
	for _, r := range p.cfg.Routers {
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
	slog.Debug("subscriber: refreshed", "total", len(next))
}

func (p *Poller) pollRouter(ctx context.Context, r RouterConfig) ([]*Subscriber, error) {
	var out []*Subscriber
	if subs, err := p.fetchPPPoE(ctx, r); err == nil {
		out = append(out, subs...)
	} else if !isNotFound(err) {
		// Surface non-404 errors (auth/network/etc.). 404 just means the
		// router does not run PPP — fall through to DHCP without warning.
		return nil, fmt.Errorf("pppoe: %w", err)
	}
	if subs, err := p.fetchDHCPLeases(ctx, r); err == nil {
		out = append(out, subs...)
	} else if !isNotFound(err) {
		return out, fmt.Errorf("dhcp: %w", err)
	}
	return out, nil
}

// ppp/active entry shape from the Mikrotik REST API.
type pppActive struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Service   string `json:"service"`
	Address   string `json:"address"`
	CallerID  string `json:"caller-id"`
	Encoding  string `json:"encoding"`
	Comment   string `json:"comment"`
	Uptime    string `json:"uptime"`
}

func (p *Poller) fetchPPPoE(ctx context.Context, r RouterConfig) ([]*Subscriber, error) {
	var rows []pppActive
	if err := p.getJSON(ctx, r, "/rest/ppp/active", &rows); err != nil {
		return nil, err
	}
	out := make([]*Subscriber, 0, len(rows))
	now := time.Now()
	for _, row := range rows {
		ip, err := netip.ParseAddr(strings.TrimSpace(row.Address))
		if err != nil || !ip.IsValid() {
			continue
		}
		ip = ip.Unmap()
		out = append(out, &Subscriber{
			IP:             ip,
			Username:       row.Name,
			Service:        "pppoe",
			Router:         r.Name,
			Comment:        firstNonEmpty(row.Comment, row.CallerID),
			ConnectedSince: subtractUptime(now, row.Uptime),
		})
	}
	return out, nil
}

// ip/dhcp-server/lease entry shape (only the fields we use).
type dhcpLease struct {
	ID         string `json:"id"`
	Address    string `json:"address"`
	Status     string `json:"status"`
	HostName   string `json:"host-name"`
	MACAddress string `json:"mac-address"`
	Server     string `json:"server"`
	Comment    string `json:"comment"`
}

func (p *Poller) fetchDHCPLeases(ctx context.Context, r RouterConfig) ([]*Subscriber, error) {
	var rows []dhcpLease
	if err := p.getJSON(ctx, r, "/rest/ip/dhcp-server/lease", &rows); err != nil {
		return nil, err
	}
	out := make([]*Subscriber, 0, len(rows))
	for _, row := range rows {
		// Only consider bound (= currently in-use) leases. Mikrotik also
		// shows "waiting", "offered", and historic leases via this endpoint.
		if row.Status != "bound" {
			continue
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(row.Address))
		if err != nil || !ip.IsValid() {
			continue
		}
		ip = ip.Unmap()
		name := firstNonEmpty(row.HostName, row.MACAddress)
		out = append(out, &Subscriber{
			IP:       ip,
			Username: name,
			Service:  "dhcp",
			Router:   r.Name,
			Comment:  firstNonEmpty(row.Comment, row.MACAddress),
		})
	}
	return out, nil
}

func (p *Poller) getJSON(ctx context.Context, r RouterConfig, path string, dst any) error {
	u, err := joinURL(r.URL, path)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(r.Username, r.Password)
	req.Header.Set("Accept", "application/json")

	// Per-router TLS settings — build a one-shot transport when verify is off.
	client := p.client
	if !r.VerifyTLS {
		client = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			},
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized — check username/password for router %q", r.Name)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────

var errNotFound = errors.New("endpoint not present on this router")

func isNotFound(err error) bool { return errors.Is(err, errNotFound) }

func joinURL(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String(), nil
}

func firstNonEmpty(a, b string) string {
	a = strings.TrimSpace(a)
	if a != "" {
		return a
	}
	return strings.TrimSpace(b)
}

// subtractUptime parses Mikrotik's "1w2d3h4m5s" format and returns the
// approximate connection-start time. Best-effort — returns zero on parse
// failure (the dashboard treats zero as "unknown").
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
