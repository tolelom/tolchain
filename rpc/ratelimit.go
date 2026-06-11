package rpc

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// rateLimitCleanupInterval controls how often stale entries are removed.
	rateLimitCleanupInterval = 5 * time.Minute
	// rateLimitEntryTTL is how long an idle IP entry is kept.
	rateLimitEntryTTL = 10 * time.Minute

	// CodeRateLimited is the JSON-RPC error code returned when rate limited.
	CodeRateLimited = -32001
)

// ipLimiter tracks per-IP token buckets for rate limiting.
type ipLimiter struct {
	mu      sync.Mutex
	entries map[string]*bucket
	rate    float64 // tokens per second
	burst   int     // max tokens
	stopCh  chan struct{}
}

// bucket is a simple token bucket for one IP.
type bucket struct {
	tokens   float64
	lastTime time.Time
}

// newIPLimiter creates a rate limiter that allows rate requests/sec with burst capacity.
func newIPLimiter(rate float64, burst int) *ipLimiter {
	l := &ipLimiter{
		entries: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
		stopCh:  make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// allow checks if a request from ip is allowed.
func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.entries[ip]
	if !ok {
		b = &bucket{tokens: float64(l.burst), lastTime: now}
		l.entries[ip] = b
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.lastTime = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// stop terminates the background cleanup goroutine.
func (l *ipLimiter) stop() {
	close(l.stopCh)
}

// cleanupLoop periodically removes stale entries.
func (l *ipLimiter) cleanupLoop() {
	ticker := time.NewTicker(rateLimitCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case now := <-ticker.C:
			l.mu.Lock()
			for ip, b := range l.entries {
				if now.Sub(b.lastTime) > rateLimitEntryTTL {
					delete(l.entries, ip)
				}
			}
			l.mu.Unlock()
		}
	}
}

// extractIP returns the IP portion of a RemoteAddr (strips port).
func extractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// parseTrustedNets converts a list of IPs/CIDRs into networks. A plain IP is
// treated as a /32 (or /128 for IPv6). Malformed entries are skipped —
// config.Validate rejects them before they can reach here.
func parseTrustedNets(entries []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			if _, n, err := net.ParseCIDR(e); err == nil {
				nets = append(nets, n)
			}
			continue
		}
		ip := net.ParseIP(e)
		if ip == nil {
			continue
		}
		bits := 128
		if ip.To4() != nil {
			bits = 32
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return nets
}

// clientIP returns the client identity used for rate limiting.
//
// When the request's peer (RemoteAddr) belongs to one of the trusted proxy
// networks and an X-Forwarded-For header is present, the LAST (rightmost)
// hop of the header is used: that is the address the trusted proxy itself
// appended — the only value it vouches for. Earlier (leftmost) entries are
// client-controlled and trivially spoofable, so they are never used.
//
// With trusted empty (the default), or for any peer outside the trusted
// networks, X-Forwarded-For is ignored and RemoteAddr is used, preserving
// the original behavior for directly exposed nodes.
func clientIP(r *http.Request, trusted []*net.IPNet) string {
	remote := extractIP(r)
	if len(trusted) == 0 {
		return remote
	}
	remoteIP := net.ParseIP(remote)
	if remoteIP == nil {
		return remote
	}
	isTrusted := false
	for _, n := range trusted {
		if n.Contains(remoteIP) {
			isTrusted = true
			break
		}
	}
	if !isTrusted {
		return remote
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remote
	}
	hops := strings.Split(xff, ",")
	last := strings.TrimSpace(hops[len(hops)-1])
	if net.ParseIP(last) == nil {
		// Malformed forwarded value: fall back to the proxy address rather
		// than letting garbage become a rate-limit key.
		return remote
	}
	return last
}
