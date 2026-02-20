package rpc

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
)

// Server is a JSON-RPC 2.0 HTTP server.
type Server struct {
	handler *Handler
	addr    string
	srv     *http.Server
}

// NewServer creates a Server on addr.
func NewServer(addr string, handler *Handler) *Server {
	s := &Server{handler: handler, addr: addr}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serveHTTP)
	s.srv = &http.Server{Addr: addr, Handler: mux}
	return s
}

// Start binds the port synchronously (so callers know immediately if binding
// fails) then serves requests in a background goroutine.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[rpc] server error: %v", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() error {
	return s.srv.Close()
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
		return
	}

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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
