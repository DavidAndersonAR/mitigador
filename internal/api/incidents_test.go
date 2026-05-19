package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/detect"
	"github.com/mitigador/mitigador/internal/incident"
	"github.com/oklog/ulid/v2"
)

// setupAuthClient sets up a server, creates a user, logs in, and returns the
// authenticated client + csrf token + server URL.
func setupAuthClient(t *testing.T) (srvURL string, client *http.Client, csrfToken string) {
	t.Helper()
	srv, _, store := setupServer(t)
	username, password := createTestUser(t, store)

	c := srv.Client()
	jar := newCookieJar(t)
	c.Jar = jar

	body, _ := json.Marshal(map[string]string{"Username": username, "Password": password})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login: want 204, got %d", resp.StatusCode)
	}

	// Get CSRF token.
	csrfResp, err := c.Get(srv.URL + "/api/auth/csrf")
	if err != nil {
		t.Fatalf("csrf: %v", err)
	}
	var csrfPayload map[string]string
	_ = json.NewDecoder(csrfResp.Body).Decode(&csrfPayload)
	csrfResp.Body.Close()

	return srv.URL, c, csrfPayload["token"]
}

// insertTestIncident inserts a minimal incident into the store for testing.
func insertTestIncident(t *testing.T, store *incident.Store, hostIP string, vector detect.Vector) string {
	t.Helper()
	id := ulid.MustNew(ulid.Now(), ulid.DefaultEntropy()).String()
	addr, _ := netip.ParseAddr(hostIP)
	ev := detect.AttackEvent{
		IncidentID: id,
		State:      detect.StateStarted,
		HostIP:     addr,
		Vector:     vector,
		Hostgroup:  "test",
		Pps:        1000,
		Bps:        8000000,
		PeakPps:    1000,
		PeakBps:    8000000,
		Confidence: 0.9,
		StartedAt:  time.Now(),
		Now:        time.Now(),
	}
	if err := store.Create(context.Background(), ev); err != nil {
		t.Fatalf("insertTestIncident: %v", err)
	}
	return id
}

// TestIncidents_ListReturnsItemsAndTotal verifies the list endpoint returns items and total.
func TestIncidents_ListReturnsItemsAndTotal(t *testing.T) {
	testDSN(t) // skip if no DSN
	srvURL, client, _ := setupAuthClient(t)

	resp, err := client.Get(srvURL + "/api/incidents")
	if err != nil {
		t.Fatalf("GET /api/incidents: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := payload["items"]; !ok {
		t.Error("missing 'items' field")
	}
	if _, ok := payload["total"]; !ok {
		t.Error("missing 'total' field")
	}
}

// TestIncidents_FilterByVector verifies the vector filter accepts valid values.
func TestIncidents_FilterByVector(t *testing.T) {
	testDSN(t)
	srvURL, client, _ := setupAuthClient(t)

	resp, err := client.Get(srvURL + "/api/incidents?vector=udp_flood")
	if err != nil {
		t.Fatalf("GET /api/incidents?vector=udp_flood: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

// TestIncidents_FilterByHostIP_InvalidReturns400 verifies invalid host_ip returns 400.
func TestIncidents_FilterByHostIP_InvalidReturns400(t *testing.T) {
	testDSN(t)
	srvURL, client, _ := setupAuthClient(t)

	resp, err := client.Get(srvURL + "/api/incidents?host_ip=not-an-ip")
	if err != nil {
		t.Fatalf("GET /api/incidents?host_ip=not-an-ip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	var payload map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload["param"] != "host_ip" {
		t.Errorf("expected param=host_ip, got %q", payload["param"])
	}
}

// TestIncidents_ActiveOnly verifies the active=true filter is accepted.
func TestIncidents_ActiveOnly(t *testing.T) {
	testDSN(t)
	srvURL, client, _ := setupAuthClient(t)

	resp, err := client.Get(srvURL + "/api/incidents?active=true")
	if err != nil {
		t.Fatalf("GET /api/incidents?active=true: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

// TestIncidents_GetByID_Returns404ForMissing verifies 404 for a missing incident ID.
func TestIncidents_GetByID_Returns404ForMissing(t *testing.T) {
	testDSN(t)
	srvURL, client, _ := setupAuthClient(t)

	resp, err := client.Get(srvURL + "/api/incidents/01NONEXISTENTULID000000000")
	if err != nil {
		t.Fatalf("GET /api/incidents/:id: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}
