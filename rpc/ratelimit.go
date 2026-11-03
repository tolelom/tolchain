package rpc

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	rateLimitCleanupInterval = 5 * time.Minute
	rateLimitEntryTTL        = 10 * time.Minute
)

// ipLimiter is a token bucket per client IP.
type ipLimiter struct {
	mu      sync.Mutex
	entries map[string]*bucket
	rate    float64 // tokens per second
	burst   int
	stopCh  chan struct{}
	stopped sync.Once
}

type bucket struct {
	tokens   float64
	lastTime time.Time
}

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

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.entries[ip]
	if !ok {
		b = &bucket{tokens: float64(l.burst), lastTime: now}
		l.entries[ip] = b
	}

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

func (l *ipLimiter) stop() {
	l.stopped.Do(func() { close(l.stopCh) })
}

// cleanupLoop forgets idle IPs so the map cannot grow without bound.
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

func extractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// parseTrustedNets treats a bare IP as a single-host network. Malformed
// entries are skipped; config.Validate rejects them first.
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

// clientIP is the rate-limit identity: RemoteAddr, or, when RemoteAddr is a
// trusted proxy, the rightmost X-Forwarded-For hop. Only that hop was added
// by the proxy; everything left of it is client-controlled.
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
		return remote
	}
	return last
}
