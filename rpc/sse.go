package rpc

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/tolelom/tolchain/events"
)

// SSEBroker manages SSE client connections and broadcasts chain events.
type SSEBroker struct {
	mu             sync.Mutex
	clients        map[chan events.Event]map[events.EventType]bool // nil filter = all events
	authToken      string
	eventBufSize   int
}

// NewSSEBroker creates a broker that subscribes to all event types on the emitter.
// sseBufferSize controls the per-client event channel capacity.
func NewSSEBroker(emitter *events.Emitter, authToken string, sseBufferSize int) *SSEBroker {
	b := &SSEBroker{
		clients:      make(map[chan events.Event]map[events.EventType]bool),
		authToken:    authToken,
		eventBufSize: sseBufferSize,
	}
	allTypes := []events.EventType{
		events.EventBlockCommit, events.EventTxExecuted, events.EventTokenTransfer,
		events.EventAssetMinted, events.EventAssetBurned, events.EventAssetTransfer,
		events.EventTemplateReg, events.EventSessionOpen, events.EventSessionClose,
		events.EventMarketList, events.EventMarketBuy, events.EventMarketCancel,
		events.EventSessionCancel, events.EventItemEquipped, events.EventItemUnequipped,
		events.EventRewardGranted, events.EventRandomCommit, events.EventRandomReveal,
	}
	for _, t := range allTypes {
		typ := t
		emitter.Subscribe(typ, func(ev events.Event) {
			b.broadcast(ev)
		})
	}
	return b
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
			// slow client — drop event
		}
	}
}

// ServeHTTP handles SSE connections. Query param: ?types=event1,event2
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if b.authToken != "" {
		if r.Header.Get("Authorization") != "Bearer "+b.authToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

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
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			data, err := json.Marshal(ev)
			if err != nil {
				slog.Error("SSE marshal event failed", "error", err)
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
			flusher.Flush()
		}
	}
}
