package rpc

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tolelom/tolchain/events"
)

const maxSSEClients = 100

// SSEBroker streams chain events to SSE clients. A client whose buffer is
// full misses events rather than slowing block processing.
type SSEBroker struct {
	mu           sync.Mutex
	clients      map[chan events.Event]map[events.EventType]bool // nil filter = all events
	authToken    string
	eventBufSize int
	clientCount  atomic.Int32
	done         chan struct{}
	closeOnce    sync.Once
}

// NewSSEBroker subscribes to every event type; sseBufferSize is the
// per-client queue length.
func NewSSEBroker(emitter *events.Emitter, authToken string, sseBufferSize int) *SSEBroker {
	b := &SSEBroker{
		clients:      make(map[chan events.Event]map[events.EventType]bool),
		authToken:    authToken,
		eventBufSize: sseBufferSize,
		done:         make(chan struct{}),
	}
	for _, typ := range events.EventTypeValues() {
		emitter.Subscribe(typ, b.broadcast)
	}
	return b
}

// Close ends every open stream; the server calls it on shutdown.
func (b *SSEBroker) Close() {
	b.closeOnce.Do(func() { close(b.done) })
}

func (b *SSEBroker) broadcast(ev events.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch, filter := range b.clients {
		if filter != nil && !filter[ev.Type] {
			continue
		}
		select {
		case ch <- ev:
		default:
		}
	}
}

// ServeHTTP streams events, optionally filtered by ?types=a,b.
//
// There is no per-IP rate limit: streams are long-lived, so connection limits
// belong to the reverse proxy in front of the node.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clear the server's WriteTimeout or it would cut the stream. Writers
	// without deadline support keep their default, hence the ignored error.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	if !bearerAuthorized(r, b.authToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if b.clientCount.Add(1) > maxSSEClients {
		b.clientCount.Add(-1)
		http.Error(w, "too many SSE clients", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		b.clientCount.Add(-1)
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := make(chan events.Event, b.eventBufSize)
	var filter map[events.EventType]bool
	if types := r.URL.Query().Get("types"); types != "" {
		filter = make(map[events.EventType]bool)
		for _, t := range strings.Split(types, ",") {
			filter[events.EventType(strings.TrimSpace(t))] = true
		}
	}

	b.mu.Lock()
	b.clients[ch] = filter
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
		b.clientCount.Add(-1)
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.done:
			return
		case ev := <-ch:
			data, err := json.Marshal(ev)
			if err != nil {
				slog.Error("SSE marshal event failed", "error", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
