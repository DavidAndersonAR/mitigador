package api_test

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/aggregate"
	"github.com/mitigador/mitigador/internal/detect"
	"github.com/mitigador/mitigador/internal/flow"
)

// corpCatalog builds a Catalog with one hostgroup "corp" covering 10.0.0.0/24.
func corpCatalog(t *testing.T) *detect.Catalog {
	t.Helper()
	prefix, err := netip.ParsePrefix("10.0.0.0/24")
	if err != nil {
		t.Fatalf("parse prefix: %v", err)
	}
	return detect.NewCatalogFromThresholds([]detect.Threshold{{
		HostgroupID: 1, HostgroupName: "corp", Prefix: prefix,
		Vector: detect.VectorUDPFlood, PPS: 1000, BPS: 1e8, MinWindowSec: 5, GraceSec: 60,
	}})
}

// seedStore injects one UDP flow record for ip at now into the store.
func seedStore(s *aggregate.Store, ip string, nowSec int64, bytes uint64) {
	addr := netip.MustParseAddr(ip)
	s.Update(addr, nowSec, flow.Record{DstIP: addr, Packets: 10, Bytes: bytes, Proto: flow.ProtoUDP})
}

// setupTrafficClient creates an authenticated HTTP client wired to a server with
// the provided store and catalog injected.
func setupTrafficClient(t *testing.T, store *aggregate.Store, catalog *detect.Catalog) (srvURL string, client *http.Client) {
	t.Helper()
	testDSN(t)
	srv, _, userStore, _, _ := setupServerWithTraffic(t, store, catalog)
	username, password := createTestUser(t, userStore)
	c := srv.Client()
	c.Jar = newCookieJar(t)
	body := strings.NewReader(`{"Username":"` + username + `","Password":"` + password + `"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login: want 204 got %d", resp.StatusCode)
	}
	return srv.URL, c
}

// ===== auth =====

func TestTraffic_Top20_RequiresAuth(t *testing.T) {
	testDSN(t)
	srv, _, _, _, _ := setupServerWithTraffic(t, nil, nil)
	resp, err := srv.Client().Get(srv.URL + "/api/traffic/top20")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestTraffic_Host_RequiresAuth(t *testing.T) {
	testDSN(t)
	srv, _, _, _, _ := setupServerWithTraffic(t, nil, nil)
	resp, err := srv.Client().Get(srv.URL + "/api/traffic/host/10.0.0.1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

// ===== top20 =====

func TestTraffic_Top20_EmptyStore_Returns200WithEmptyItems(t *testing.T) {
	store := aggregate.New(1)
	srvURL, c := setupTrafficClient(t, store, nil)
	resp, err := c.Get(srvURL + "/api/traffic/top20")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var payload struct {
		Items       []map[string]any `json:"items"`
		GeneratedAt string           `json:"generated_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 0 {
		t.Fatalf("want 0 items, got %d", len(payload.Items))
	}
	if payload.GeneratedAt == "" {
		t.Fatal("generated_at missing")
	}
}

func TestTraffic_Top20_OrdersByBpsDesc(t *testing.T) {
	store := aggregate.New(1)
	nowSec := time.Now().Unix()
	seedStore(store, "10.0.0.1", nowSec, 1000)
	seedStore(store, "10.0.0.2", nowSec, 9000)
	seedStore(store, "10.0.0.3", nowSec, 5000)
	srvURL, c := setupTrafficClient(t, store, nil)
	resp, err := c.Get(srvURL + "/api/traffic/top20")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if len(payload.Items) != 3 {
		t.Fatalf("want 3, got %d", len(payload.Items))
	}
	if payload.Items[0]["ip"] != "10.0.0.2" {
		t.Errorf("want top ip=10.0.0.2, got %v", payload.Items[0]["ip"])
	}
	if payload.Items[1]["ip"] != "10.0.0.3" {
		t.Errorf("want second ip=10.0.0.3, got %v", payload.Items[1]["ip"])
	}
	if payload.Items[2]["ip"] != "10.0.0.1" {
		t.Errorf("want third ip=10.0.0.1, got %v", payload.Items[2]["ip"])
	}
	// dominant_proto must be present
	for _, it := range payload.Items {
		if it["dominant_proto"] == nil {
			t.Error("dominant_proto missing")
		}
	}
}

func TestTraffic_Top20_ResolvesHostgroupViaCatalog(t *testing.T) {
	store := aggregate.New(1)
	seedStore(store, "10.0.0.5", time.Now().Unix(), 4000)
	srvURL, c := setupTrafficClient(t, store, corpCatalog(t))
	resp, err := c.Get(srvURL + "/api/traffic/top20")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if len(payload.Items) == 0 {
		t.Fatal("want at least 1 item")
	}
	if payload.Items[0]["hostgroup"] != "corp" {
		t.Fatalf("want hostgroup=corp, got %v", payload.Items[0]["hostgroup"])
	}
}

func TestTraffic_Top20_NullHostgroupWhenNoMatch(t *testing.T) {
	store := aggregate.New(1)
	seedStore(store, "172.16.0.1", time.Now().Unix(), 4000) // outside 10.0.0.0/24
	srvURL, c := setupTrafficClient(t, store, corpCatalog(t))
	resp, err := c.Get(srvURL + "/api/traffic/top20")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if len(payload.Items) == 0 {
		t.Fatal("want at least 1 item")
	}
	if payload.Items[0]["hostgroup"] != nil {
		t.Fatalf("want null hostgroup, got %v", payload.Items[0]["hostgroup"])
	}
}

// ===== host =====

func TestTraffic_Host_InvalidIP_Returns400(t *testing.T) {
	srvURL, c := setupTrafficClient(t, aggregate.New(1), nil)
	resp, err := c.Get(srvURL + "/api/traffic/host/not-an-ip")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	var payload map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload["error"] != "invalid_param" || payload["param"] != "ip" {
		t.Fatalf("unexpected error payload: %+v", payload)
	}
}

func TestTraffic_Host_OversizeIP_Returns400(t *testing.T) {
	srvURL, c := setupTrafficClient(t, aggregate.New(1), nil)
	long := strings.Repeat("a", 65)
	resp, err := c.Get(srvURL + "/api/traffic/host/" + long)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestTraffic_Host_UnknownHost_Returns404(t *testing.T) {
	srvURL, c := setupTrafficClient(t, aggregate.New(1), nil)
	resp, err := c.Get(srvURL + "/api/traffic/host/10.0.0.99")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
	var payload map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload["error"] != "not_found" {
		t.Fatalf("unexpected error: %+v", payload)
	}
}

func TestTraffic_Host_KnownHost_Returns60Buckets(t *testing.T) {
	store := aggregate.New(1)
	nowSec := time.Now().Unix()
	seedStore(store, "10.0.0.1", nowSec, 8000)
	srvURL, c := setupTrafficClient(t, store, corpCatalog(t))
	resp, err := c.Get(srvURL + "/api/traffic/host/10.0.0.1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var payload struct {
		IP        string           `json:"ip"`
		Hostgroup *string          `json:"hostgroup"`
		Buckets   []map[string]any `json:"buckets"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload.IP != "10.0.0.1" {
		t.Errorf("want ip=10.0.0.1, got %s", payload.IP)
	}
	if payload.Hostgroup == nil || *payload.Hostgroup != "corp" {
		t.Errorf("want hostgroup=corp, got %v", payload.Hostgroup)
	}
	if len(payload.Buckets) != 60 {
		t.Fatalf("want 60 buckets, got %d", len(payload.Buckets))
	}
	// offset_s monotonic 0..59
	for i, b := range payload.Buckets {
		// JSON numbers decode as float64 in map[string]any.
		v, ok := b["offset_s"].(float64)
		if !ok || int(v) != i {
			t.Fatalf("bucket %d: offset_s mismatch: %v", i, b["offset_s"])
		}
	}
}
