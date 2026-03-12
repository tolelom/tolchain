package core

import "errors"

// Sentinel errors for blockchain operations.
var (
	// ErrNotFound is returned when a requested object does not exist in storage.
	ErrNotFound = errors.New("not found")

	// ErrInsufficientBalance indicates an account has insufficient balance.
	ErrInsufficientBalance = errors.New("insufficient balance")

	// ErrInvalidNonce indicates the transaction nonce does not match expected.
	ErrInvalidNonce = errors.New("invalid nonce")

	// ErrBalanceOverflow indicates a balance update would overflow uint64.
	ErrBalanceOverflow = errors.New("balance overflow")

	// ErrNonceOverflow indicates the nonce has reached maximum value.
	ErrNonceOverflow = errors.New("nonce overflow")

	// ErrAlreadyExists indicates the resource already exists.
	ErrAlreadyExists = errors.New("already exists")

	// ErrUnauthorized indicates the sender lacks permission.
	ErrUnauthorized = errors.New("unauthorized")
)
