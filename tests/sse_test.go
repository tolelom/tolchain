package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/rpc"
)

func TestSSEAuthRequired(t *testing.T) {
	emitter := events.NewEmitter()
	broker := rpc.NewSSEBroker(emitter, "secret-token", 64)

	server := httptest.NewServer(broker)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", resp.StatusCode)
	}
}

func TestSSEBrokerBroadcast(t *testing.T) {
	emitter := events.NewEmitter()
	broker := rpc.NewSSEBroker(emitter, "", 64)

	// Use a pipe-based recorder that supports flushing
	pr, pw := newPipeResponseWriter()

	req := httptest.NewRequest("GET", "/?types=asset_minted,market_buy", nil)

	// Serve in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		broker.ServeHTTP(pw, req)
	}()

	// Wait for subscription to register
	time.Sleep(50 * time.Millisecond)

	// Emit events
	emitter.Emit(events.Event{
		Type: events.EventAssetMinted, TxID: "tx-001",
		Data: map[string]any{"asset_id": "a1"},
	})
	emitter.Emit(events.Event{
		Type: events.EventMarketBuy, TxID: "tx-002",
		Data: map[string]any{"listing_id": "l1"},
	})
	// Non-matching event
	emitter.Emit(events.Event{
		Type: events.EventTokenTransfer, TxID: "tx-filtered",
	})

	// Give time for events to be written
	time.Sleep(100 * time.Millisecond)

	// Read what was written
	output := pr.String()

	if !strings.Contains(output, "event: asset_minted") {
		t.Errorf("missing asset_minted event in output: %s", output)
	}
	if !strings.Contains(output, "tx-001") {
		t.Errorf("missing tx-001 in output: %s", output)
	}
	if !strings.Contains(output, "event: market_buy") {
		t.Errorf("missing market_buy event: %s", output)
	}
	if strings.Contains(output, "tx-filtered") {
		t.Error("should not contain filtered event")
	}
}

// pipeResponseWriter implements http.ResponseWriter + http.Flusher by writing
// to a buffer. The handler goroutine writes while the test goroutine reads, so
// the buffer is mutex-protected.
type pipeResponseWriter struct {
	header http.Header
	mu     sync.Mutex
	buf    strings.Builder
	status int
}

func newPipeResponseWriter() (*pipeResponseWriter, *pipeResponseWriter) {
	w := &pipeResponseWriter{header: make(http.Header)}
	return w, w
}

func (w *pipeResponseWriter) Header() http.Header { return w.header }
func (w *pipeResponseWriter) WriteHeader(code int) { w.status = code }
func (w *pipeResponseWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(b)
}
func (w *pipeResponseWriter) Flush() {} // no-op, data is already in buffer
func (w *pipeResponseWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
