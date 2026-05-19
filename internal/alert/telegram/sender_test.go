package telegram_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gobot "github.com/go-telegram/bot"

	"github.com/mitigador/mitigador/internal/alert/telegram"
	"github.com/mitigador/mitigador/internal/config"
	"github.com/mitigador/mitigador/internal/detect"
)

func mustParseAddr(s string) netip.Addr {
	a, err := netip.ParseAddr(s)
	if err != nil {
		panic(err)
	}
	return a
}

// fakeTelegramServer creates an httptest.Server that proxies to handler.
// Note: go-telegram/bot sends requests as multipart/form-data, not JSON.
// Use r.FormValue("field") in handlers to read parameters.
func fakeTelegramServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

// okHandler returns HTTP 200 with a successful Telegram API response.
// It increments the calls counter and reads the body to avoid broken-pipe.
func okHandler(calls *atomic.Int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 1},
		})
	}
}

func makeCfg(token string, chatIDs []int64) config.Telegram {
	return config.Telegram{
		BotToken:       token,
		AllowedChatIDs: chatIDs,
	}
}

func makeStartedEvent() detect.AttackEvent {
	return detect.AttackEvent{
		IncidentID: "01HY00000000000000000001",
		State:      detect.StateStarted,
		HostIP:     mustParseAddr("192.0.2.1"),
		Vector:     detect.VectorUDPFlood,
		Hostgroup:  "test-group",
		Pps:        5000,
		Bps:        40000000,
		PeakPps:    5000,
		PeakBps:    40000000,
		StartedAt:  time.Now(),
		Now:        time.Now(),
	}
}

func TestSender_FansToAllChatIDs(t *testing.T) {
	var calls atomic.Int64
	srv := fakeTelegramServer(t, okHandler(&calls))
	defer srv.Close()

	chatIDs := []int64{100, 200, 300}
	cfg := makeCfg("test-bot-token-1234567890", chatIDs)

	s, err := telegram.NewSender(cfg, "https://example.com", gobot.WithServerURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	in := make(chan detect.AttackEvent, 1)
	in <- makeStartedEvent()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = s.Run(ctx, in) }()

	// Wait for all 3 chat IDs to receive (one request each).
	deadline := time.After(4 * time.Second)
	for calls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("expected 3 API calls (one per chat ID), got %d", calls.Load())
		case <-time.After(50 * time.Millisecond):
		}
	}

	if got := calls.Load(); got != 3 {
		t.Errorf("expected exactly 3 sendMessage calls, got %d", got)
	}
}

func TestSender_RetriesOn429(t *testing.T) {
	var calls atomic.Int64
	// First call returns 429 with retry_after=1; subsequent calls return 200.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{
				"ok":          false,
				"error_code":  429,
				"description": "Too Many Requests: retry after 1",
				"parameters":  map[string]any{"retry_after": 1},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 1},
		})
	})

	srv := fakeTelegramServer(t, handler)
	defer srv.Close()

	cfg := makeCfg("test-bot-token-1234567890", []int64{100})
	s, err := telegram.NewSender(cfg, "https://example.com", gobot.WithServerURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	in := make(chan detect.AttackEvent, 1)
	in <- makeStartedEvent()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() { _ = s.Run(ctx, in) }()

	// Expect at least 2 API calls: first 429, then success on retry.
	deadline := time.After(8 * time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("expected 2 API calls (retry after 429), got %d", calls.Load())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestSender_OnlyAllowedChatIDsReceive(t *testing.T) {
	// Verifies ONLY the configured AllowedChatIDs receive messages.
	// go-telegram/bot sends multipart/form-data — use r.FormValue("chat_id").
	var mu sync.Mutex
	var receivedChatIDs []int64
	var totalCalls atomic.Int64

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ParseMultipartForm so r.FormValue works for both
		// multipart/form-data and application/x-www-form-urlencoded.
		_ = r.ParseMultipartForm(1 << 20)
		raw := r.FormValue("chat_id")
		if raw != "" {
			if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
				mu.Lock()
				receivedChatIDs = append(receivedChatIDs, id)
				mu.Unlock()
				totalCalls.Add(1)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 1},
		})
	})

	srv := fakeTelegramServer(t, handler)
	defer srv.Close()

	allowedIDs := []int64{111, 222}
	cfg := makeCfg("test-bot-token-1234567890", allowedIDs)
	s, err := telegram.NewSender(cfg, "https://example.com", gobot.WithServerURL(srv.URL))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	in := make(chan detect.AttackEvent, 1)
	in <- makeStartedEvent()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = s.Run(ctx, in) }()

	// Wait until both sends are confirmed via atomic counter.
	deadline := time.After(4 * time.Second)
	for totalCalls.Load() < int64(len(allowedIDs)) {
		select {
		case <-deadline:
			t.Fatalf("only received %d sends, want %d", totalCalls.Load(), len(allowedIDs))
		case <-time.After(50 * time.Millisecond):
		}
	}

	mu.Lock()
	got := append([]int64(nil), receivedChatIDs...)
	mu.Unlock()

	for _, chatID := range got {
		found := false
		for _, want := range allowedIDs {
			if chatID == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("message sent to unauthorized chat_id %d (not in allowedIDs)", chatID)
		}
	}
}
