package api_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mitigador/mitigador/internal/api"
	"github.com/mitigador/mitigador/internal/detect"
)

// TestBroker_FanOut connects 3 test clients, publishes 1 event, verifies all receive.
func TestBroker_FanOut(t *testing.T) {
	in := make(chan detect.AttackEvent, 4)
	broker := api.NewBroker(in)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = broker.Run(ctx) }()

	// Give the broker a moment to start.
	time.Sleep(10 * time.Millisecond)

	addr, _ := netip.ParseAddr("10.0.0.1")
	ev := detect.AttackEvent{
		IncidentID: "01TEST",
		State:      detect.StateStarted,
		HostIP:     addr,
		Vector:     detect.VectorUDPFlood,
		Pps:        1000,
		Bps:        8000000,
		Now:        time.Now(),
	}

	var wg sync.WaitGroup
	receivedCount := 0
	var mu sync.Mutex

	const numClients = 3
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		srv := httptest.NewServer(http.HandlerFunc(broker.Handler))
		t.Cleanup(srv.Close)

		go func(srvURL string) {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srvURL, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.HasPrefix(line, "event:") {
					mu.Lock()
					receivedCount++
					mu.Unlock()
					return
				}
			}
		}(srv.URL)
	}

	// Allow clients to connect.
	time.Sleep(50 * time.Millisecond)

	// Publish event.
	in <- ev

	// Wait for goroutines with timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for clients to receive event")
	}

	mu.Lock()
	got := receivedCount
	mu.Unlock()
	if got != numClients {
		t.Errorf("want %d clients to receive event, got %d", numClients, got)
	}
}

// TestBroker_Heartbeat_15s verifies heartbeat is sent approximately every 15s.
// We use a short timeout for CI — just check the heartbeat line format.
func TestBroker_Heartbeat_15s(t *testing.T) {
	// Use a very short tick for testing — we create a broker with a pipe
	// and check that the heartbeat comment is written.
	in := make(chan detect.AttackEvent, 4)
	broker := api.NewBroker(in)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = broker.Run(ctx) }()
	time.Sleep(10 * time.Millisecond)

	// Set up a test server with a recorder response writer.
	pr, pw := io.Pipe()
	w := &flusherWriter{pw: pw}

	// Run handler in a goroutine; it will block until we cancel.
	handlerCtx, cancelHandler := context.WithTimeout(ctx, 16*time.Second)
	defer cancelHandler()
	req, _ := http.NewRequestWithContext(handlerCtx, http.MethodGet, "/", nil)

	go func() {
		broker.Handler(w, req)
	}()

	// Read lines until we see a heartbeat or time out.
	scanner := bufio.NewScanner(pr)
	found := false
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for scanner.Scan() {
			line := scanner.Text()
			if line == ": heartbeat" {
				found = true
				return
			}
		}
	}()

	select {
	case <-readDone:
	case <-time.After(16 * time.Second):
		// timed out waiting for heartbeat — only fail if not found
	}
	cancelHandler()

	if !found {
		t.Error("no heartbeat received within 16s")
	}
}

// flusherWriter implements http.ResponseWriter + http.Flusher over a pipe.
type flusherWriter struct {
	pw      *io.PipeWriter
	header  http.Header
	once    sync.Once
}

func (f *flusherWriter) Header() http.Header {
	f.once.Do(func() { f.header = make(http.Header) })
	return f.header
}

func (f *flusherWriter) Write(b []byte) (int, error) {
	return f.pw.Write(b)
}

func (f *flusherWriter) WriteHeader(_ int) {}

func (f *flusherWriter) Flush() {}

// TestBroker_DropsOnSlowClient verifies that slow clients don't block fast ones.
func TestBroker_DropsOnSlowClient(t *testing.T) {
	in := make(chan detect.AttackEvent, 32)
	broker := api.NewBroker(in)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = broker.Run(ctx) }()
	time.Sleep(10 * time.Millisecond)

	addr, _ := netip.ParseAddr("10.0.0.1")

	// Fast client — reads events.
	fastSrv := httptest.NewServer(http.HandlerFunc(broker.Handler))
	t.Cleanup(fastSrv.Close)

	fastReceived := 0
	var fastMu sync.Mutex
	fastCtx, cancelFast := context.WithCancel(ctx)
	defer cancelFast()

	go func() {
		req, _ := http.NewRequestWithContext(fastCtx, http.MethodGet, fastSrv.URL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "event:") {
				fastMu.Lock()
				fastReceived++
				fastMu.Unlock()
			}
		}
	}()

	// Slow client — connects but never reads.
	slowSrv := httptest.NewServer(http.HandlerFunc(broker.Handler))
	t.Cleanup(slowSrv.Close)

	slowCtx, cancelSlow := context.WithCancel(ctx)
	go func() {
		req, _ := http.NewRequestWithContext(slowCtx, http.MethodGet, slowSrv.URL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		// Intentionally block — never read.
		<-slowCtx.Done()
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish 20 events — more than the 16-event buffer for the slow client.
	for i := 0; i < 20; i++ {
		in <- detect.AttackEvent{
			IncidentID: "01TEST",
			State:      detect.StateStarted,
			HostIP:     addr,
			Vector:     detect.VectorUDPFlood,
			Now:        time.Now(),
		}
	}

	// Disconnect slow client after flooding.
	cancelSlow()
	time.Sleep(100 * time.Millisecond)

	// Fast client should have received events without hanging.
	fastMu.Lock()
	got := fastReceived
	fastMu.Unlock()
	if got == 0 {
		t.Error("fast client received 0 events — may be blocked by slow client")
	}
}

// TestHandler_HeadersSet verifies SSE response headers using a real HTTP
// connection so that the response headers arrive before the streaming body
// blocks. We connect, read the response line + headers, then cancel.
func TestHandler_HeadersSet(t *testing.T) {
	in := make(chan detect.AttackEvent, 4)
	broker := api.NewBroker(in)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = broker.Run(ctx) }()
	time.Sleep(10 * time.Millisecond)

	// Use a transport with a short response-header timeout and manual redirect.
	// The SSE handler writes headers immediately then blocks on events, so a
	// context deadline causes Do() to return ctx.Err() with a non-nil Response
	// only if the server already wrote headers. Use a separate goroutine to
	// cancel after headers are expected to have arrived.
	_, pw := io.Pipe()
	w := &flusherWriter{pw: pw}

	reqCtx, cancelReq := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelReq()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, "/", nil)

	// Run handler directly — it sets headers before blocking on events.
	done := make(chan struct{})
	go func() {
		defer close(done)
		broker.Handler(w, req)
	}()

	// Headers are set synchronously before the handler blocks, so they are
	// visible immediately via the header map.
	time.Sleep(20 * time.Millisecond)

	h := w.Header()
	ct := h.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type: want text/event-stream, got %q", ct)
	}
	if cc := h.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control: want no-cache, got %q", cc)
	}
	if xab := h.Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("X-Accel-Buffering: want no, got %q", xab)
	}

	// Cancel so the handler goroutine exits.
	cancelReq()
	pw.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler goroutine did not exit")
	}
}
