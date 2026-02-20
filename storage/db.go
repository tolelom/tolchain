package storage

// Batch is an atomic write buffer. All operations are applied together
// via Write() or discarded together on error, preventing partial commits.
type Batch interface {
	Set(key, value []byte)
	Delete(key []byte)
	Write() error
	Reset()
}

// DB is the generic key-value store interface.
type DB interface {
	Get(key []byte) ([]byte, error)
	Set(key, value []byte) error
	Delete(key []byte) error
	NewIterator(prefix []byte) Iterator
	NewBatch() Batch
	Close() error
}

// Iterator walks key-value pairs matching a prefix.
type Iterator interface {
	Next() bool
	Key() []byte
	Value() []byte
	Release()
	Error() error
}
