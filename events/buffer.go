package events

type EventEmitter interface {
	Emit(ev Event)
}

// Buffer holds events until the block is committed, so subscribers never see
// events of state that is rolled back. It is not safe for concurrent use.
type Buffer struct {
	events []Event
}

func NewBuffer() *Buffer {
	return &Buffer{}
}

func (b *Buffer) Emit(ev Event) {
	b.events = append(b.events, ev)
}

func (b *Buffer) Flush(dst EventEmitter) {
	for _, ev := range b.events {
		dst.Emit(ev)
	}
	b.events = b.events[:0]
}

func (b *Buffer) Discard() {
	b.events = b.events[:0]
}
