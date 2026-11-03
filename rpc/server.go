package rpc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/tolelom/tolchain/metrics"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
)

// Server serves JSON-RPC 2.0 on "/", plus /status, /metrics and optionally
// the SSE stream on /events.
type Server struct {
	handler     *Handler
	addr        string
	authToken   string // empty → no auth required
	statusFunc  func() any
	srv         *http.Server
	ln          net.Listener
	limiter     *ipLimiter
	maxBodySize int64
	trustedNets []*net.IPNet
}

// NewServer builds the server. A non-empty authToken requires
// "Authorization: Bearer <token>" on RPC, /metrics and /events; /status stays
// open. trustedProxies (IPs/CIDRs) are peers whose last X-Forwarded-For hop is
// used as the rate-limit identity.
func NewServer(addr string, handler *Handler, authToken string, sseBroker *SSEBroker, statusFunc func() any, rateLimitVal float64, rateBurstVal int, readTimeoutSec, writeTimeoutSec, idleTimeoutSec int, maxBodySize int64, trustedProxies []string) *Server {
	s := &Server{
		handler:     handler,
		addr:        addr,
		authToken:   authToken,
		statusFunc:  statusFunc,
		limiter:     newIPLimiter(rateLimitVal, rateBurstVal),
		maxBodySize: maxBodySize,
		trustedNets: parseTrustedNets(trustedProxies),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serveHTTP)
	if sseBroker != nil {
		mux.Handle("/events", sseBroker)
	}
	if statusFunc != nil {
		mux.HandleFunc("/status", s.serveStatus)
	}
	mux.HandleFunc("/metrics", s.serveMetrics)
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       time.Duration(readTimeoutSec) * time.Second,
		WriteTimeout:      time.Duration(writeTimeoutSec) * time.Second,
		IdleTimeout:       time.Duration(idleTimeoutSec) * time.Second,
	}
	if sseBroker != nil {
		s.srv.RegisterOnShutdown(sseBroker.Close)
	}
	return s
}

// Start binds synchronously so a bind error is returned, then serves in the
// background.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.ln = ln
	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("RPC server error", "error", err)
		}
	}()
	return nil
}

func (s *Server) Addr() net.Addr {
	if s.ln != nil {
		return s.ln.Addr()
	}
	return nil
}

func (s *Server) Stop() error {
	s.limiter.stop()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

// setCORS allows any origin only while no auth token is configured; with a
// token, browsers must go through a same-origin proxy.
func (s *Server) setCORS(w http.ResponseWriter, methods string) {
	if s.authToken == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.Header().Set("Access-Control-Allow-Methods", methods)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// bearerAuthorized compares in constant time so the token cannot be guessed
// byte by byte from response timing.
func bearerAuthorized(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	got := r.Header.Get("Authorization")
	return subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+token)) == 1
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.setCORS(w, "GET, POST, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.limiter.allow(clientIP(r, s.trustedNets)) {
		metrics.RPCRateLimited.Inc()
		w.Header().Set("Retry-After", "1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(w, errResponse(nil, CodeRateLimited, "rate limit exceeded"))
		return
	}

	if !bearerAuthorized(r, s.authToken) {
		metrics.RPCErrors.WithLabelValues(fmt.Sprint(CodeUnauthorized)).Inc()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, errResponse(nil, CodeUnauthorized, "unauthorized"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodySize)

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		metrics.RPCErrors.WithLabelValues(fmt.Sprint(CodeParseError)).Inc()
		writeJSON(w, errResponse(nil, CodeParseError, err.Error()))
		return
	}
	if req.JSONRPC != "2.0" {
		metrics.RPCErrors.WithLabelValues(fmt.Sprint(CodeInvalidRequest)).Inc()
		writeJSON(w, errResponse(req.ID, CodeInvalidRequest, "jsonrpc must be '2.0'"))
		return
	}

	start := time.Now()
	resp := s.handler.Dispatch(req)
	method := req.Method
	if resp.Error != nil && resp.Error.Code == CodeMethodNotFound {
		method = "unknown" // bounds metric label cardinality
	}
	metrics.RPCRequests.WithLabelValues(method).Inc()
	metrics.RPCDuration.WithLabelValues(method).Observe(time.Since(start).Seconds())
	if resp.Error != nil {
		metrics.RPCErrors.WithLabelValues(fmt.Sprint(resp.Error.Code)).Inc()
	}
	writeJSON(w, resp)
}

func (s *Server) serveStatus(w http.ResponseWriter, r *http.Request) {
	s.setCORS(w, "GET, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.limiter.allow(clientIP(r, s.trustedNets)) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	writeJSON(w, s.statusFunc())
}

func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	if !bearerAuthorized(r, s.authToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	metrics.Handler().ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("RPC write response failed", "error", err)
	}
}
