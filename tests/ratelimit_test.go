package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tolelom/tolchain/rpc"
)

// TestRateLimitExceeded verifies that exceeding the rate limit returns 429.
func TestRateLimitExceeded(t *testing.T) {
	handler := newTestRPCHandler(t)
	// Start server with rate limit applied (default: 100 req/s, burst 200).
	srv := rpc.NewServer(":0", handler, "", nil, nil, 100, 200, 30, 30, 60, 1024*1024, nil)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	url := "http://" + srv.Addr().String() + "/"

	makeReq := func() int {
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"method":  "getBlockHeight",
			"params":  map[string]any{},
			"id":      1,
		})
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		io.ReadAll(resp.Body)
		return resp.StatusCode
	}

	// Send enough requests to exhaust burst + any tokens refilled during the loop.
	// Burst=200, rate=100/s. The loop takes ~50-100ms, so ~5-10 tokens refill.
	// Sending 250 guarantees exhaustion.
	rateLimited := false
	for i := 0; i < 250; i++ {
		code := makeReq()
		if code == http.StatusTooManyRequests {
			rateLimited = true
			t.Logf("rate limited at request %d", i)
			break
		}
		if code != http.StatusOK {
			t.Fatalf("request %d: unexpected status %d", i, code)
		}
	}
	if !rateLimited {
		t.Error("expected rate limiting after burst, but all 250 requests succeeded")
	}
}

// TestMetricsEndpoint verifies that /metrics returns Prometheus metrics.
func TestMetricsEndpoint(t *testing.T) {
	handler := newTestRPCHandler(t)
	srv := rpc.NewServer(":0", handler, "", nil, nil, 100, 200, 30, 30, 60, 1024*1024, nil)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	url := "http://" + srv.Addr().String() + "/metrics"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics: status %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	// Verify our custom metrics are present.
	for _, want := range []string{
		"tolchain_block_height",
		"tolchain_mempool_size",
		"tolchain_rpc_requests_total",
		"tolchain_peers_connected",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("/metrics missing %q", want)
		}
	}
}

// TestRateLimitAllowsNormalTraffic ensures moderate traffic is not blocked.
func TestRateLimitAllowsNormalTraffic(t *testing.T) {
	handler := newTestRPCHandler(t)
	srv := rpc.NewServer(":0", handler, "", nil, nil, 100, 200, 30, 30, 60, 1024*1024, nil)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	url := "http://" + srv.Addr().String() + "/"

	// Send 10 requests — well within limits.
	for i := 0; i < 10; i++ {
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"method":  "getMempoolSize",
			"params":  map[string]any{},
			"id":      i,
		})
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: got status %d, want 200", i, resp.StatusCode)
		}
	}
}
