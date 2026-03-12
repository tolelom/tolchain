package vm

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/tolelom/tolchain/core"
)

func TestRegistry_RegisterAndExecute(t *testing.T) {
	r := NewRegistry()
	typ := core.TxType("reg_test_exec")
	called := false

	r.Register(typ, func(ctx *Context, payload json.RawMessage) error {
		called = true
		return nil
	})

	err := r.Execute(typ, &Context{}, nil)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestRegistry_ExecuteUnregisteredType(t *testing.T) {
	r := NewRegistry()
	err := r.Execute(core.TxType("nonexistent"), &Context{}, nil)
	if err == nil {
		t.Fatal("expected error for unregistered type, got nil")
	}
	expected := `vm: no handler registered for TxType "nonexistent"`
	if err.Error() != expected {
		t.Fatalf("unexpected error message: got %q, want %q", err.Error(), expected)
	}
}

func TestRegistry_DuplicateRegisterPanics(t *testing.T) {
	r := NewRegistry()
	typ := core.TxType("dup_test")
	r.Register(typ, func(ctx *Context, payload json.RawMessage) error { return nil })

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected panic on duplicate registration, got none")
		}
		msg := fmt.Sprintf("%v", rec)
		expected := `vm: handler already registered for TxType "dup_test"`
		if msg != expected {
			t.Fatalf("unexpected panic message: got %q, want %q", msg, expected)
		}
	}()

	r.Register(typ, func(ctx *Context, payload json.RawMessage) error { return nil })
}

func TestRegistry_MultipleHandlers(t *testing.T) {
	r := NewRegistry()
	typA := core.TxType("multi_a")
	typB := core.TxType("multi_b")

	var calledA, calledB bool

	r.Register(typA, func(ctx *Context, payload json.RawMessage) error {
		calledA = true
		return nil
	})
	r.Register(typB, func(ctx *Context, payload json.RawMessage) error {
		calledB = true
		return nil
	})

	// Execute A
	if err := r.Execute(typA, &Context{}, nil); err != nil {
		t.Fatalf("Execute typA: %v", err)
	}
	if !calledA {
		t.Fatal("handler A was not called")
	}
	if calledB {
		t.Fatal("handler B should not have been called yet")
	}

	// Execute B
	if err := r.Execute(typB, &Context{}, nil); err != nil {
		t.Fatalf("Execute typB: %v", err)
	}
	if !calledB {
		t.Fatal("handler B was not called")
	}
}

func TestRegistry_HandlerReturnsError(t *testing.T) {
	r := NewRegistry()
	typ := core.TxType("err_handler")
	handlerErr := errors.New("handler failed")

	r.Register(typ, func(ctx *Context, payload json.RawMessage) error {
		return handlerErr
	})

	err := r.Execute(typ, &Context{}, nil)
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected handler error, got: %v", err)
	}
}

func TestRegistry_PayloadPassthrough(t *testing.T) {
	r := NewRegistry()
	typ := core.TxType("payload_test")
	input := json.RawMessage(`{"key":"value"}`)
	var received json.RawMessage

	r.Register(typ, func(ctx *Context, payload json.RawMessage) error {
		received = payload
		return nil
	})

	if err := r.Execute(typ, &Context{}, input); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(received) != string(input) {
		t.Fatalf("payload mismatch: got %s, want %s", received, input)
	}
}
