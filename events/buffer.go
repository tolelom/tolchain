package events

// EventEmitter is the interface used by the executor to emit events.
// Both *Emitter and *Buffer satisfy this interface.
type EventEmitter interface {
	Emit(ev Event)
}

// Buffer collects events during block execution and replays them to a real
// emitter after the block has been committed. This prevents subscribers from
// seeing events for state that might be rolled back.
type Buffer struct {
	events []Event
}

// NewBuffer creates an empty event buffer.
func NewBuffer() *Buffer {
	return &Buffer{}
}

// Emit appends an event to the buffer without delivering it.
func (b *Buffer) Emit(ev Event) {
	b.events = append(b.events, ev)
}

// Flush delivers all buffered events to dst and clears the buffer.
func (b *Buffer) Flush(dst EventEmitter) {
	for _, ev := range b.events {
		dst.Emit(ev)
	}
	b.events = b.events[:0]
}

// Discard clears all buffered events without delivering them.
func (b *Buffer) Discard() {
	b.events = b.events[:0]
}
