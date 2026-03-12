package events

import (
	"sync/atomic"
	"testing"
)

func TestEmitter_SubscribeAndEmit(t *testing.T) {
	em := NewEmitter()
	var called int32

	em.Subscribe(EventTokenTransfer, func(ev Event) {
		atomic.AddInt32(&called, 1)
	})

	em.Emit(Event{Type: EventTokenTransfer})
	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("handler should be called once")
	}
}

func TestEmitter_MultipleSubscribers(t *testing.T) {
	em := NewEmitter()
	var count int32

	em.Subscribe(EventAssetMinted, func(ev Event) { atomic.AddInt32(&count, 1) })
	em.Subscribe(EventAssetMinted, func(ev Event) { atomic.AddInt32(&count, 1) })

	em.Emit(Event{Type: EventAssetMinted})
	if atomic.LoadInt32(&count) != 2 {
		t.Fatal("both handlers should be called")
	}
}

func TestEmitter_DifferentTypes_NoInterference(t *testing.T) {
	em := NewEmitter()
	var called int32

	em.Subscribe(EventAssetMinted, func(ev Event) { atomic.AddInt32(&called, 1) })
	em.Emit(Event{Type: EventAssetBurned}) // different type

	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("handler should not be called for different event type")
	}
}

func TestEmitter_PanicRecovery(t *testing.T) {
	em := NewEmitter()
	var secondCalled int32

	em.Subscribe(EventBlockCommit, func(ev Event) {
		panic("boom")
	})
	em.Subscribe(EventBlockCommit, func(ev Event) {
		atomic.AddInt32(&secondCalled, 1)
	})

	// Should not panic.
	em.Emit(Event{Type: EventBlockCommit})

	if atomic.LoadInt32(&secondCalled) != 1 {
		t.Fatal("second handler should still be called after first panics")
	}
}

func TestEmitter_EventData(t *testing.T) {
	em := NewEmitter()
	var received Event

	em.Subscribe(EventTxExecuted, func(ev Event) {
		received = ev
	})
	em.Emit(Event{
		Type:        EventTxExecuted,
		TxID:        "tx123",
		BlockHeight: 42,
		Data:        map[string]any{"key": "value"},
	})

	if received.TxID != "tx123" || received.BlockHeight != 42 {
		t.Fatal("event data should be passed correctly")
	}
	if received.Data["key"] != "value" {
		t.Fatal("event data map should be passed correctly")
	}
}

func TestBuffer_EmitAndFlush(t *testing.T) {
	buf := NewBuffer()
	em := NewEmitter()
	var count int32

	em.Subscribe(EventTokenTransfer, func(ev Event) { atomic.AddInt32(&count, 1) })

	buf.Emit(Event{Type: EventTokenTransfer})
	buf.Emit(Event{Type: EventTokenTransfer})

	// Not yet delivered.
	if atomic.LoadInt32(&count) != 0 {
		t.Fatal("buffer should not deliver events before Flush")
	}

	buf.Flush(em)
	if atomic.LoadInt32(&count) != 2 {
		t.Fatal("Flush should deliver all buffered events")
	}
}

func TestBuffer_Discard(t *testing.T) {
	buf := NewBuffer()
	em := NewEmitter()
	var count int32

	em.Subscribe(EventTokenTransfer, func(ev Event) { atomic.AddInt32(&count, 1) })

	buf.Emit(Event{Type: EventTokenTransfer})
	buf.Discard()
	buf.Flush(em)

	if atomic.LoadInt32(&count) != 0 {
		t.Fatal("Discard should clear buffered events")
	}
}

func TestBuffer_FlushClearsBuffer(t *testing.T) {
	buf := NewBuffer()
	em := NewEmitter()
	var count int32

	em.Subscribe(EventTokenTransfer, func(ev Event) { atomic.AddInt32(&count, 1) })

	buf.Emit(Event{Type: EventTokenTransfer})
	buf.Flush(em)
	buf.Flush(em) // second flush should deliver nothing

	if atomic.LoadInt32(&count) != 1 {
		t.Fatal("Flush should clear buffer after delivery")
	}
}
