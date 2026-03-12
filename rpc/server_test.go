package rpc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/indexer"
	"github.com/tolelom/tolchain/internal/testutil"
)

func newTestServer(authToken string) *Server {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	return NewServer(":0", handler, authToken, nil, func() any {
		return map[string]string{"status": "ok"}
	}, 100, 200, 30, 30, 60, 1024*1024)
}

func TestServer_StartAndStop(t *testing.T) {
	s := newTestServer("")
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if s.Addr() == nil {
		t.Fatal("expected non-nil Addr after Start")
	}
}

func TestServer_ServeHTTP_MethodNotAllowed(t *testing.T) {
	s := newTestServer("")
	defer s.limiter.stop()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.serveHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_ServeHTTP_ValidRequest(t *testing.T) {
	s := newTestServer("")
	defer s.limiter.stop()

	body, _ := json.Marshal(Request{JSONRPC: "2.0", ID: 1, Method: "getBlockHeight"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.serveHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	if resp.JSONRPC != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
}

func TestServer_ServeHTTP_InvalidJSON(t *testing.T) {
	s := newTestServer("")
	defer s.limiter.stop()

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("{invalid")))
	w := httptest.NewRecorder()
	s.serveHTTP(w, req)

	// The server writes a JSON-RPC error response with parse error code.
	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected parse error response")
	}
	if resp.Error.Code != CodeParseError {
		t.Fatalf("expected code %d, got %d", CodeParseError, resp.Error.Code)
	}
}

func TestServer_ServeHTTP_InvalidJSONRPC(t *testing.T) {
	s := newTestServer("")
	defer s.limiter.stop()

	body, _ := json.Marshal(Request{JSONRPC: "1.0", ID: 1, Method: "getBlockHeight"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.serveHTTP(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected invalid request error")
	}
	if resp.Error.Code != CodeInvalidRequest {
		t.Fatalf("expected code %d, got %d", CodeInvalidRequest, resp.Error.Code)
	}
}

func TestServer_ServeHTTP_AuthRequired(t *testing.T) {
	s := newTestServer("secret-token")
	defer s.limiter.stop()

	body, _ := json.Marshal(Request{JSONRPC: "2.0", ID: 1, Method: "getBlockHeight"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.serveHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestServer_ServeHTTP_AuthValid(t *testing.T) {
	s := newTestServer("secret-token")
	defer s.limiter.stop()

	body, _ := json.Marshal(Request{JSONRPC: "2.0", ID: 1, Method: "getBlockHeight"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()
	s.serveHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestServer_ServeHTTP_AuthWrongToken(t *testing.T) {
	s := newTestServer("secret-token")
	defer s.limiter.stop()

	body, _ := json.Marshal(Request{JSONRPC: "2.0", ID: 1, Method: "getBlockHeight"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	s.serveHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestServer_ServeStatus(t *testing.T) {
	s := newTestServer("")
	defer s.limiter.stop()

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.serveStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode status response: %v", err)
	}
	if result["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", result["status"])
	}
}

func TestServer_ServeStatus_MethodNotAllowed(t *testing.T) {
	s := newTestServer("")
	defer s.limiter.stop()

	req := httptest.NewRequest(http.MethodPost, "/status", nil)
	w := httptest.NewRecorder()
	s.serveStatus(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_ServeStatus_RateLimited(t *testing.T) {
	s := newTestServer("")
	defer s.limiter.stop()

	// Exhaust rate limiter for a specific IP
	for i := 0; i < 250; i++ {
		s.limiter.allow("1.2.3.4")
	}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	w := httptest.NewRecorder()
	s.serveStatus(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", w.Code)
	}
}

func TestServer_ServeHTTP_RateLimited(t *testing.T) {
	s := newTestServer("")
	defer s.limiter.stop()

	// Exhaust rate limiter
	for i := 0; i < 250; i++ {
		s.limiter.allow("5.6.7.8")
	}

	body, _ := json.Marshal(Request{JSONRPC: "2.0", ID: 1, Method: "getBlockHeight"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.RemoteAddr = "5.6.7.8:12345"
	w := httptest.NewRecorder()
	s.serveHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", w.Code)
	}
}
