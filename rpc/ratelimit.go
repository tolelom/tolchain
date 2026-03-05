package rpc

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// rateLimit is the sustained requests per second allowed per IP.
	rateLimit = 100
	// rateBurst is the maximum burst size above the sustained rate.
	rateBurst = 200
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
