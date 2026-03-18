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
	// readHeaderTimeout limits how long the server waits for request headers.
	readHeaderTimeout = 10 * time.Second
	// shutdownTimeout is the grace period for in-flight requests during shutdown.
	shutdownTimeout = 5 * time.Second
)

// Server is a JSON-RPC 2.0 HTTP server.
type Server struct {
	handler        *Handler
	addr           string
	authToken      string // empty → no auth required
	statusFunc     func() any
	srv            *http.Server
	ln             net.Listener
	limiter        *ipLimiter
	maxBodySize    int64
}

// NewServer creates a Server on addr. If authToken is non-empty, every
// request must carry a matching "Authorization: Bearer <token>" header.
// If sseBroker is non-nil, an SSE endpoint is registered at /events.
// If statusFunc is non-nil, a GET /status endpoint returns its result as JSON.
// rateLimit, rateBurst, readTimeoutSec, writeTimeoutSec, idleTimeoutSec, maxBodySize
// control server behavior.
func NewServer(addr string, handler *Handler, authToken string, sseBroker *SSEBroker, statusFunc func() any, rateLimitVal float64, rateBurstVal int, readTimeoutSec, writeTimeoutSec, idleTimeoutSec int, maxBodySize int64) *Server {
	s := &Server{
		handler:     handler,
		addr:        addr,
		authToken:   authToken,
		statusFunc:  statusFunc,
		limiter:     newIPLimiter(rateLimitVal, rateBurstVal),
		maxBodySize: maxBodySize,
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
	return s
}

// Start binds the port synchronously (so callers know immediately if binding
// fails) then serves requests in a background goroutine.
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

// Addr returns the listener's address. Useful when started on ":0".
func (s *Server) Addr() net.Addr {
	if s.ln != nil {
		return s.ln.Addr()
	}
	return nil
}

// Stop gracefully shuts down the HTTP server, waiting up to 5 seconds for
// in-flight requests to complete.
func (s *Server) Stop() error {
	s.limiter.stop()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS headers for cross-origin requests.
	if s.authToken == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	// When auth is required, do not allow wildcard CORS.
	// Clients must use a reverse proxy or same-origin requests.
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.limiter.allow(extractIP(r)) {
		metrics.RPCRateLimited.Inc()
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(w, errResponse(nil, CodeRateLimited, "rate limit exceeded"))
		return
	}

	if s.authToken != "" {
		got := r.Header.Get("Authorization")
		expected := "Bearer " + s.authToken
		if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			metrics.RPCErrors.WithLabelValues(fmt.Sprint(CodeUnauthorized)).Inc()
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, errResponse(nil, CodeUnauthorized, "unauthorized"))
			return
		}
	}

	// Limit request body to prevent memory exhaustion.
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
	metrics.RPCRequests.WithLabelValues(req.Method).Inc()
	metrics.RPCDuration.WithLabelValues(req.Method).Observe(time.Since(start).Seconds())
	if resp.Error != nil {
		metrics.RPCErrors.WithLabelValues(fmt.Sprint(resp.Error.Code)).Inc()
	}
	writeJSON(w, resp)
}

func (s *Server) serveStatus(w http.ResponseWriter, r *http.Request) {
	// Apply same CORS policy as the main RPC handler.
	if s.authToken == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.limiter.allow(extractIP(r)) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	writeJSON(w, s.statusFunc())
}

func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	if s.authToken != "" {
		got := r.Header.Get("Authorization")
		expected := "Bearer " + s.authToken
		if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	metrics.Handler().ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("RPC write response failed", "error", err)
	}
}
