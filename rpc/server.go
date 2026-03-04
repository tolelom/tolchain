package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server is a JSON-RPC 2.0 HTTP server.
type Server struct {
	handler    *Handler
	addr       string
	authToken  string // empty → no auth required
	statusFunc func() any
	srv        *http.Server
	ln         net.Listener
}

// NewServer creates a Server on addr. If authToken is non-empty, every
// request must carry a matching "Authorization: Bearer <token>" header.
// If sseBroker is non-nil, an SSE endpoint is registered at /events.
// If statusFunc is non-nil, a GET /status endpoint returns its result as JSON.
func NewServer(addr string, handler *Handler, authToken string, sseBroker *SSEBroker, statusFunc func() any) *Server {
	s := &Server{handler: handler, addr: addr, authToken: authToken, statusFunc: statusFunc}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serveHTTP)
	if sseBroker != nil {
		mux.Handle("/events", sseBroker)
	}
	if statusFunc != nil {
		mux.HandleFunc("/status", s.serveStatus)
	}
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.authToken != "" {
		if r.Header.Get("Authorization") != "Bearer "+s.authToken {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, errResponse(nil, CodeUnauthorized, "unauthorized"))
			return
		}
	}

	// Limit request body to 1 MB to prevent memory exhaustion.
	r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, errResponse(nil, CodeParseError, err.Error()))
		return
	}
	if req.JSONRPC != "2.0" {
		writeJSON(w, errResponse(req.ID, CodeInvalidRequest, "jsonrpc must be '2.0'"))
		return
	}

	resp := s.handler.Dispatch(req)
	writeJSON(w, resp)
}

func (s *Server) serveStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.statusFunc())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("RPC write response failed", "error", err)
	}
}
