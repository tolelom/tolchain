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

// ---- clientIP (reverse proxy trust) ----

func xffRequest(remoteAddr, xff string) *http.Request {
	r := &http.Request{RemoteAddr: remoteAddr, Header: http.Header{}}
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIP_NoTrustedProxies_UsesRemoteAddr(t *testing.T) {
	// 기본(빈 trusted_proxies) = 기존 동작: XFF가 있어도 RemoteAddr 사용.
	r := xffRequest("203.0.113.5:1234", "1.1.1.1")
	if got := clientIP(r, nil); got != "203.0.113.5" {
		t.Fatalf("got %q, want 203.0.113.5", got)
	}
}

func TestClientIP_UntrustedPeer_IgnoresXFF(t *testing.T) {
	nets := parseTrustedNets([]string{"10.0.0.0/8"})
	r := xffRequest("203.0.113.5:1234", "1.1.1.1")
	if got := clientIP(r, nets); got != "203.0.113.5" {
		t.Fatalf("got %q, want 203.0.113.5", got)
	}
}

func TestClientIP_TrustedPeer_MultiHopXFF_UsesLastHop(t *testing.T) {
	// rightmost 값이 신뢰 프록시가 직접 붙인(보증하는) 유일한 홉이다.
	nets := parseTrustedNets([]string{"10.0.0.1"})
	r := xffRequest("10.0.0.1:9999", "1.1.1.1, 2.2.2.2, 3.3.3.3")
	if got := clientIP(r, nets); got != "3.3.3.3" {
		t.Fatalf("got %q, want 3.3.3.3", got)
	}
}

func TestClientIP_TrustedPeer_SingleXFF(t *testing.T) {
	nets := parseTrustedNets([]string{"10.0.0.1"})
	r := xffRequest("10.0.0.1:9999", "198.51.100.7")
	if got := clientIP(r, nets); got != "198.51.100.7" {
		t.Fatalf("got %q, want 198.51.100.7", got)
	}
}

func TestClientIP_TrustedPeer_NoXFF_UsesRemoteAddr(t *testing.T) {
	nets := parseTrustedNets([]string{"10.0.0.1"})
	r := xffRequest("10.0.0.1:9999", "")
	if got := clientIP(r, nets); got != "10.0.0.1" {
		t.Fatalf("got %q, want 10.0.0.1", got)
	}
}

func TestClientIP_TrustedPeerByCIDR(t *testing.T) {
	nets := parseTrustedNets([]string{"172.16.0.0/12"})
	r := xffRequest("172.18.0.5:1234", "198.51.100.7")
	if got := clientIP(r, nets); got != "198.51.100.7" {
		t.Fatalf("got %q, want 198.51.100.7", got)
	}
}

func TestClientIP_TrustedPeer_MalformedXFF_FallsBackToRemoteAddr(t *testing.T) {
	nets := parseTrustedNets([]string{"10.0.0.1"})
	r := xffRequest("10.0.0.1:9999", "garbage, not-an-ip")
	if got := clientIP(r, nets); got != "10.0.0.1" {
		t.Fatalf("got %q, want 10.0.0.1", got)
	}
}

func TestClientIP_TrustedPeer_IPv6(t *testing.T) {
	nets := parseTrustedNets([]string{"::1"})
	r := xffRequest("[::1]:9999", "2001:db8::7")
	if got := clientIP(r, nets); got != "2001:db8::7" {
		t.Fatalf("got %q, want 2001:db8::7", got)
	}
}
