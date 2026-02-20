package storage

// DB is the generic key-value store interface.
type DB interface {
	Get(key []byte) ([]byte, error)
	Set(key, value []byte) error
	Delete(key []byte) error
	NewIterator(prefix []byte) Iterator
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
