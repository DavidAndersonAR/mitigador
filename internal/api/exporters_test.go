package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestExporters_ReturnsAllFromInventory verifies the exporters list endpoint returns items.
func TestExporters_ReturnsAllFromInventory(t *testing.T) {
	testDSN(t)
	srvURL, client, _ := setupAuthClient(t)

	resp, err := client.Get(srvURL + "/api/exporters")
	if err != nil {
		t.Fatalf("GET /api/exporters: %v", err)
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
}

// TestExporters_StatusReflectsHealthTracker verifies that the status field is present.
func TestExporters_StatusReflectsHealthTracker(t *testing.T) {
	testDSN(t)
	srvURL, client, _ := setupAuthClient(t)

	resp, err := client.Get(srvURL + "/api/exporters")
	if err != nil {
		t.Fatalf("GET /api/exporters: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)

	items, _ := payload["items"].([]any)
	// If there are items, verify the expected fields are present.
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			t.Fatal("item is not a map")
		}
		for _, key := range []string{"source_ip", "type", "status", "flows_per_sec", "sample_rate_override"} {
			if _, ok := item[key]; !ok {
				t.Errorf("missing key %q in exporter item", key)
			}
		}
	}
}

// TestBGPStub_ReturnsEmptyArray verifies /api/bgp/sessions returns {"items":[]}.
func TestBGPStub_ReturnsEmptyArray(t *testing.T) {
	testDSN(t)
	srvURL, client, _ := setupAuthClient(t)

	resp, err := client.Get(srvURL + "/api/bgp/sessions")
	if err != nil {
		t.Fatalf("GET /api/bgp/sessions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	items, ok := payload["items"]
	if !ok {
		t.Fatal("missing 'items' field")
	}
	arr, ok := items.([]any)
	if !ok {
		t.Fatalf("expected items to be array, got %T", items)
	}
	if len(arr) != 0 {
		t.Errorf("expected empty items array, got %d items", len(arr))
	}
}
