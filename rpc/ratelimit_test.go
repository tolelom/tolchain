package rpc

import (
	"net/http"
	"testing"
	"time"
)

func TestAllow_FirstRequestsUpToBurst(t *testing.T) {
	l := newIPLimiter(10, 5)
	defer l.stop()

	for i := 0; i < 5; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed (within burst)", i)
		}
	}
}

func TestAllow_AfterBurstExhausted_ReturnsFalse(t *testing.T) {
	l := newIPLimiter(10, 3)
	defer l.stop()

	// Exhaust burst
	for i := 0; i < 3; i++ {
		l.allow("1.2.3.4")
	}

	if l.allow("1.2.3.4") {
		t.Fatal("should be denied after burst exhausted")
	}
}

func TestAllow_DifferentIPs_SeparateBuckets(t *testing.T) {
	l := newIPLimiter(10, 2)
	defer l.stop()

	// Exhaust burst for IP A
	l.allow("1.1.1.1")
	l.allow("1.1.1.1")
	if l.allow("1.1.1.1") {
		t.Fatal("1.1.1.1 should be denied")
	}

	// IP B should still be allowed
	if !l.allow("2.2.2.2") {
		t.Fatal("2.2.2.2 should be allowed (separate bucket)")
	}
}

func TestAllow_TokensRefillOverTime(t *testing.T) {
	// High rate so tokens refill quickly
	l := newIPLimiter(1000, 1)
	defer l.stop()

	// Use the one token
	l.allow("1.2.3.4")
	if l.allow("1.2.3.4") {
		t.Fatal("should be denied right after burst")
	}

	// Wait a short time for refill
	time.Sleep(10 * time.Millisecond)

	if !l.allow("1.2.3.4") {
		t.Fatal("should be allowed after token refill")
	}
}

func TestStop_DoesNotPanic(t *testing.T) {
	l := newIPLimiter(10, 5)
	l.stop()
	// Just verify no panic
}

func TestExtractIP_StripsPort(t *testing.T) {
	r := &http.Request{RemoteAddr: "1.2.3.4:12345"}
	if got := extractIP(r); got != "1.2.3.4" {
		t.Fatalf("expected 1.2.3.4, got %s", got)
	}
}

func TestExtractIP_NoPort(t *testing.T) {
	r := &http.Request{RemoteAddr: "1.2.3.4"}
	if got := extractIP(r); got != "1.2.3.4" {
		t.Fatalf("expected 1.2.3.4, got %s", got)
	}
}
