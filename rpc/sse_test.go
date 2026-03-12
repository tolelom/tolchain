package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tolelom/tolchain/events"
)

func TestSSEBroker_BroadcastToClient(t *testing.T) {
	emitter := events.NewEmitter()
	broker := NewSSEBroker(emitter, "", 64)

	ch := make(chan events.Event, 10)
	broker.mu.Lock()
	broker.clients[ch] = nil // nil filter = receive all
	broker.mu.Unlock()

	ev := events.Event{Type: events.EventBlockCommit, BlockHeight: 1}
	broker.broadcast(ev)

	select {
	case got := <-ch:
		if got.Type != events.EventBlockCommit {
			t.Fatalf("got type %s, want %s", got.Type, events.EventBlockCommit)
		}
		if got.BlockHeight != 1 {
			t.Fatalf("got height %d, want 1", got.BlockHeight)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Cleanup
	broker.mu.Lock()
	delete(broker.clients, ch)
	broker.mu.Unlock()
}

func TestSSEBroker_BroadcastWithFilter(t *testing.T) {
	emitter := events.NewEmitter()
	broker := NewSSEBroker(emitter, "", 64)

	ch := make(chan events.Event, 10)
	filter := map[events.EventType]bool{events.EventBlockCommit: true}
	broker.mu.Lock()
	broker.clients[ch] = filter
	broker.mu.Unlock()

	// This should NOT be delivered (wrong type)
	broker.broadcast(events.Event{Type: events.EventTokenTransfer})

	// This SHOULD be delivered
	broker.broadcast(events.Event{Type: events.EventBlockCommit, BlockHeight: 5})

	select {
	case got := <-ch:
		if got.Type != events.EventBlockCommit {
			t.Fatalf("got type %s, want block_commit", got.Type)
		}
		if got.BlockHeight != 5 {
			t.Fatalf("got height %d, want 5", got.BlockHeight)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// Verify no extra event was delivered
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra event: %v", extra)
	default:
	}

	broker.mu.Lock()
	delete(broker.clients, ch)
	broker.mu.Unlock()
}

func TestSSEBroker_BroadcastDropsSlowClient(t *testing.T) {
	emitter := events.NewEmitter()
	broker := NewSSEBroker(emitter, "", 64)

	ch := make(chan events.Event, 1) // tiny buffer
	broker.mu.Lock()
	broker.clients[ch] = nil
	broker.mu.Unlock()

	// Fill the channel
	broker.broadcast(events.Event{Type: events.EventBlockCommit, BlockHeight: 1})

	// This should not block even though the channel is full
	done := make(chan struct{})
	go func() {
		broker.broadcast(events.Event{Type: events.EventBlockCommit, BlockHeight: 2})
		close(done)
	}()

	select {
	case <-done:
		// OK — broadcast did not block
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on slow client")
	}

	broker.mu.Lock()
	delete(broker.clients, ch)
	broker.mu.Unlock()
}

func TestSSEBroker_ServeHTTP_AuthRequired(t *testing.T) {
	emitter := events.NewEmitter()
	broker := NewSSEBroker(emitter, "secret-token", 64)

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	w := httptest.NewRecorder()
	broker.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", w.Code)
	}
}

func TestSSEBroker_ServeHTTP_AuthValid(t *testing.T) {
	emitter := events.NewEmitter()
	broker := NewSSEBroker(emitter, "secret-token", 64)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		broker.ServeHTTP(w, req)
	}()

	// Give the handler time to register the client
	time.Sleep(50 * time.Millisecond)

	// Verify that the client was registered
	broker.mu.Lock()
	clientCount := len(broker.clients)
	broker.mu.Unlock()
	if clientCount != 1 {
		t.Fatalf("expected 1 client, got %d", clientCount)
	}

	// Cancel the context to stop the SSE handler
	cancel()
	<-done

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
}

func TestSSEBroker_EventViaEmitter(t *testing.T) {
	emitter := events.NewEmitter()
	broker := NewSSEBroker(emitter, "", 64)

	ch := make(chan events.Event, 10)
	broker.mu.Lock()
	broker.clients[ch] = nil
	broker.mu.Unlock()

	// Emit through the emitter (not directly via broadcast)
	emitter.Emit(events.Event{Type: events.EventBlockCommit, BlockHeight: 99})

	select {
	case got := <-ch:
		if got.BlockHeight != 99 {
			t.Fatalf("got height %d, want 99", got.BlockHeight)
		}
		if got.Type != events.EventBlockCommit {
			t.Fatalf("got type %s, want block_commit", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event via emitter")
	}

	broker.mu.Lock()
	delete(broker.clients, ch)
	broker.mu.Unlock()
}

func TestSSEBroker_ServeHTTP_TypeFilter(t *testing.T) {
	emitter := events.NewEmitter()
	broker := NewSSEBroker(emitter, "", 64)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events?types=block_commit,tx_executed", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		broker.ServeHTTP(w, req)
	}()

	// Give the handler time to register
	time.Sleep(50 * time.Millisecond)

	// Verify client registered with a filter
	broker.mu.Lock()
	var registeredFilter map[events.EventType]bool
	for _, f := range broker.clients {
		registeredFilter = f
		break
	}
	broker.mu.Unlock()

	if registeredFilter == nil {
		t.Fatal("expected non-nil filter for ?types= query")
	}
	if !registeredFilter[events.EventBlockCommit] {
		t.Error("filter should include block_commit")
	}
	if !registeredFilter[events.EventTxExecuted] {
		t.Error("filter should include tx_executed")
	}
	if registeredFilter[events.EventTokenTransfer] {
		t.Error("filter should NOT include token_transfer")
	}

	cancel()
	<-done
}
