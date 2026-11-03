// Package rpc serves JSON-RPC 2.0 over HTTP, plus status, metrics and an SSE
// event stream.
package rpc

import "encoding/json"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC codes, then this server's own (-32000 and below).
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeUnauthorized   = -32000
	CodeRateLimited    = -32001
)

func errResponse(id any, code int, msg string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: msg},
	}
}

func okResponse(id, result any) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}
