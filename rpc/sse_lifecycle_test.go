package rpc

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/indexer"
	"github.com/tolelom/tolchain/internal/testutil"
)

func startSSEServer(t *testing.T) *Server {
	t.Helper()
	bc := core.NewBlockchain(testutil.NewMemBlockStore())
	bc.Init()
	emitter := events.NewEmitter()
	handler := NewHandler(bc, core.NewMempool(100, 3600, 300), testutil.NewStateDB(), indexer.New(testutil.NewMemDB(), emitter), "test-chain")
	s := NewServer("127.0.0.1:0", handler, "", NewSSEBroker(emitter, "", 8), nil, 100, 200, 30, 30, 60, 1024*1024, nil)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	return s
}

func openStream(t *testing.T, s *Server) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.Addr().String()+"/events?types=random_reveal", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream did not open before any event arrived: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestSSE_HeadersArriveBeforeFirstEvent(t *testing.T) {
	s := startSSEServer(t)
	defer s.Stop()
	resp := openStream(t, s)
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestSSE_StopDoesNotWaitOnOpenStreams(t *testing.T) {
	s := startSSEServer(t)
	openStream(t, s)
	start := time.Now()
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop with an open stream: %v", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("Stop took %v with an open stream", took)
	}
}

func TestServer_StopTwiceDoesNotPanic(t *testing.T) {
	s := startSSEServer(t)
	s.Stop()
	s.Stop()
}
