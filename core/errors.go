package core

import "errors"

var (
	ErrNotFound            = errors.New("not found")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrInvalidNonce        = errors.New("invalid nonce")
	ErrBalanceOverflow     = errors.New("balance overflow")
	ErrNonceOverflow       = errors.New("nonce overflow")
	ErrAlreadyExists       = errors.New("already exists")
	ErrUnauthorized        = errors.New("unauthorized")
)
