//go:build integration

// Package integration_test provides an end-to-end test for the full Mitigador pipeline:
//
//	flowgen → mitigador serve (NetFlow UDP) → aggregate → detect → incident recorder → Postgres
//
// The test skips unless MITIGADOR_TEST_PG_DSN is set.
//
// Run:
//
//	MITIGADOR_TEST_PG_DSN=postgres://localhost/mitigador_test \
//	  go test -tags=integration -count=1 -v ./test/integration/ -run TestE2E
//
// Note on Cookie.Secure=true: Go's net/http CookieJar honours cookies from
// any URL (it does not enforce Secure at the client side — only browsers do).
// So the test can authenticate over plain HTTP without any flag changes to serve.
package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2E_FlowToIncident runs the full pipeline end-to-end:
//
//  1. Build mitigador and flowgen binaries to a temp dir.
//  2. Write a minimal config.yaml + domain.yaml.
//  3. Apply domain config (config sync) — creates exporters, hostgroups, thresholds.
//  4. Create a test user directly via the binary (user create).
//  5. Start mitigador serve and wait for "http: listening".
//  6. Run flowgen against the UDP listener.
//  7. Login via /api/auth/login, then poll /api/incidents?vector=udp_flood&active=true.
//  8. Assert at least one incident appears within 15 seconds.
func TestE2E_FlowToIncident(t *testing.T) {
	dsn := os.Getenv("MITIGADOR_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("MITIGADOR_TEST_PG_DSN not set — skipping E2E test")
	}

	// Pick free ports for HTTP API and the three UDP ingest listeners.
	httpPort := mustFreePort(t)
	udpPort := mustFreePort(t)   // NetFlow (flowgen targets this)
	ipfixPort := mustFreePort(t) // IPFIX (unused in this test but must be valid)
	sflowPort := mustFreePort(t) // sFlow  (unused in this test but must be valid)

	tmp := t.TempDir()

	// Build mitigador binary.
	mitBin := filepath.Join(tmp, "mitigador")
	t.Log("building mitigador...")
	if out, err := exec.Command(
		"go", "build", "-o", mitBin, "../../cmd/mitigador",
	).CombinedOutput(); err != nil {
		t.Fatalf("build mitigador: %v\n%s", err, out)
	}

	// Build flowgen binary.
	flowBin := filepath.Join(tmp, "flowgen")
	t.Log("building flowgen...")
	if out, err := exec.Command(
		"go", "build", "-o", flowBin, "../../cmd/flowgen",
	).CombinedOutput(); err != nil {
		t.Fatalf("build flowgen: %v\n%s", err, out)
	}

	// Write config.yaml.
	cfgPath := filepath.Join(tmp, "config.yaml")
	writeTestConfig(t, cfgPath, dsn, httpPort, udpPort, ipfixPort, sflowPort)

	// Write domain.yaml.
	domainPath := filepath.Join(tmp, "domain.yaml")
	writeTestDomain(t, domainPath, udpPort)

	// Apply domain config.
	t.Log("running config sync...")
	if out, err := exec.Command(
		mitBin, "--config", cfgPath, "config", "sync", "--file", domainPath,
	).CombinedOutput(); err != nil {
		t.Fatalf("config sync: %v\n%s", err, out)
	}
	t.Log("config sync OK")

	// Create test user via echo-piped stdin.
	// user create reads password from TTY; we use a helper that writes to stdin.
	testPassword := "e2etestpassword123"
	t.Log("creating test user...")
	createCmd := exec.Command(mitBin, "--config", cfgPath, "user", "create", "e2etest")
	createCmd.Stdin = strings.NewReader(testPassword + "\n" + testPassword + "\n")
	if out, err := createCmd.CombinedOutput(); err != nil {
		// User may already exist from a previous run — ignore ErrAlreadyExists.
		if !strings.Contains(string(out), "already exists") {
			t.Fatalf("user create: %v\n%s", err, out)
		}
		t.Log("user already exists, continuing")
	} else {
		t.Logf("user create: %s", out)
	}

	// Start mitigador serve.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	serveCmd := exec.CommandContext(ctx, mitBin, "--config", cfgPath, "serve")
	// Capture stdout+stderr to detect "http: listening".
	pr, pw := io.Pipe()
	serveCmd.Stdout = pw
	serveCmd.Stderr = pw

	if err := serveCmd.Start(); err != nil {
		t.Fatalf("start mitigador serve: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = serveCmd.Wait()
		_ = pw.Close()
	})

	// Wait for "http: listening" in the output (max 20s).
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	t.Logf("waiting for mitigador to be ready at %s ...", baseURL)
	if !waitForLog(t, pr, "http: listening", 20*time.Second) {
		t.Fatal("mitigador did not print 'http: listening' within 20s")
	}
	// Give the server a moment to be fully ready after logging.
	time.Sleep(200 * time.Millisecond)
	t.Log("mitigador is ready")

	// Start flowgen — send UDP flood toward 192.0.2.10 for 12 seconds.
	// The threshold is pps=50 so pps=200 should trigger detection within 5s.
	flowCmd := exec.CommandContext(ctx, flowBin,
		fmt.Sprintf("--target=127.0.0.1:%d", udpPort),
		"--src=127.0.0.1",
		"--dst=192.0.2.10",
		"--pps=200",
		"--bytes=200",
		"--duration=12s",
		"--proto=17",
		"--interval=1s",
	)
	flowCmd.Stdout = os.Stdout
	flowCmd.Stderr = os.Stderr
	if err := flowCmd.Start(); err != nil {
		t.Fatalf("start flowgen: %v", err)
	}
	t.Cleanup(func() { _ = flowCmd.Wait() })
	t.Log("flowgen started")

	// Login to obtain a session cookie.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	t.Log("logging in...")
	loginBody, _ := json.Marshal(map[string]string{
		"username": "e2etest",
		"password": testPassword,
	})
	loginResp, err := client.Post(
		baseURL+"/api/auth/login",
		"application/json",
		bytes.NewReader(loginBody),
	)
	if err != nil {
		t.Fatalf("login POST: %v", err)
	}
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", loginResp.StatusCode)
	}
	t.Logf("login OK (status %d, cookies: %v)", loginResp.StatusCode, jar.Cookies(mustParseURL(baseURL)))

	// Poll /api/incidents?vector=udp_flood&active=true until an incident appears.
	// Detection needs: min_window_sec=5 + up to 1s detector tick = ~6s.
	// Give 15s total to handle slow CI environments.
	t.Log("polling for incident (max 15s)...")
	pollCtx, pollCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer pollCancel()

	incidentURL := baseURL + "/api/incidents?vector=udp_flood&active=true&limit=10"
	found := false
	for !found {
		select {
		case <-pollCtx.Done():
			t.Fatal("timeout: no udp_flood incident appeared within 15s")
		case <-time.After(500 * time.Millisecond):
		}

		req, _ := http.NewRequestWithContext(pollCtx, http.MethodGet, incidentURL, nil)
		resp, err := client.Do(req)
		if err != nil {
			if pollCtx.Err() != nil {
				t.Fatal("timeout: no udp_flood incident appeared within 15s")
			}
			t.Logf("poll GET error (retrying): %v", err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			t.Fatalf("poll: got 401 — session cookie not sent or expired")
		}
		if resp.StatusCode != http.StatusOK {
			t.Logf("poll: status %d (retrying): %s", resp.StatusCode, body)
			continue
		}

		// Parse response — expect {"items":[...],"total":N} envelope.
		var result struct {
			Items []json.RawMessage `json:"items"`
			Total int               `json:"total"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			t.Logf("poll: unmarshal error (retrying): %v — body: %s", err, body)
			continue
		}

		if len(result.Items) > 0 {
			t.Logf("E2E PASS: found %d active udp_flood incident(s)", len(result.Items))
			found = true
		} else {
			t.Logf("poll: 0 incidents so far, waiting...")
		}
	}
}

// writeTestConfig writes a minimal config.yaml to path.
// Telegram and SMTP are configured with dummy values so serve doesn't error out.
// The real senders will fail to connect but that does not affect incident creation.
func writeTestConfig(t *testing.T, path, dsn string, httpPort, netflowPort, ipfixPort, sflowPort int) {
	t.Helper()
	// Use a 32-char session secret (minimum required by validator).
	sessionSecret := "e2e-test-session-secret-xxxxxxxx"
	content := fmt.Sprintf(`postgres:
  dsn: %q
  max_conns: 4
  min_conns: 1

http:
  listen_addr: "127.0.0.1"
  listen_port: %d
  session_secret: %q
  app_base_url: "http://127.0.0.1:%d"

ingest:
  netflow:
    listen_addr: "127.0.0.1"
    listen_port: %d
  ipfix:
    listen_addr: "127.0.0.1"
    listen_port: %d
  sflow:
    listen_addr: "127.0.0.1"
    listen_port: %d
  receive_buffer_bytes: 1048576

telegram:
  bot_token: "e2e-test-fake-token-00000000000"
  allowed_chat_ids: [123456789]

smtp:
  host: "127.0.0.1"
  port: 25
  username: "test"
  password: "test"
  security: "plain"
  from_addr: "test@example.com"
  to_addrs: ["ops@example.com"]

log:
  level: "debug"
  format: "text"
`, dsn, httpPort, sessionSecret, httpPort, netflowPort, ipfixPort, sflowPort)

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
}

// writeTestDomain writes a minimal domain.yaml to path.
// The exporter source_ip is 127.0.0.1 (what flowgen binds to).
// The threshold pps=50 is below flowgen's --pps=200 to trigger detection.
func writeTestDomain(t *testing.T, path string, netflowPort int) {
	t.Helper()
	_ = netflowPort // informational — not used in domain.yaml
	content := `exporters:
  - source_ip: "127.0.0.1"
    type: "netflow"
    sample_rate_override: 1
    description: "flowgen test exporter"

hostgroups:
  - name: "e2e"
    prefix: "192.0.2.0/24"
    description: "E2E test hostgroup (RFC 5737 documentation prefix)"

thresholds:
  - hostgroup: "e2e"
    vector: "udp_flood"
    pps: 50
    bps: 10000
    min_window_sec: 5
    grace_sec: 10

alert_channels: []
whitelist: []
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write domain.yaml: %v", err)
	}
}

// waitForLog reads from r until it finds a line containing needle, or timeout elapses.
// Returns true if found, false on timeout.
func waitForLog(t *testing.T, r io.Reader, needle string, timeout time.Duration) bool {
	t.Helper()
	found := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			t.Logf("[serve] %s", line)
			if strings.Contains(line, needle) {
				close(found)
				// Keep draining so the pipe does not block the server.
				for scanner.Scan() {
					t.Logf("[serve] %s", scanner.Text())
				}
				return
			}
		}
	}()
	select {
	case <-found:
		return true
	case <-time.After(timeout):
		return false
	}
}

// mustFreePort asks the OS for a free TCP port and returns it.
// TCP and UDP share the port space for our test purposes.
func mustFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mustFreePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// mustParseURL parses a URL or panics.
func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
