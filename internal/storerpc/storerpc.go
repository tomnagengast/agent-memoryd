// Package storerpc provides a Unix-socket JSON-RPC layer that lets CLI/MCP
// processes talk to a running daemon without opening the zvec collection
// directly (zvec takes a fully-exclusive lock on Open).
//
// Protocol: one TCP-like connection per call.  Caller sends one JSON Request
// object, server replies with one JSON Response object, connection is closed.
package storerpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

// SocketPath returns the Unix socket path for the given config.
func SocketPath(cfg config.Config) string {
	return filepath.Join(cfg.Root, "memoryd.sock")
}

// Probe returns true if a daemon is already listening at the socket path.
func Probe(cfg config.Config) bool {
	conn, err := net.DialTimeout("unix", SocketPath(cfg), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ------------------------------------------------------------------ wire types

// Request is the single JSON object sent by the client.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the single JSON object returned by the server.
type Response struct {
	Error  *RPCError       `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// RPCError carries a machine-readable code and a human-readable message.
type RPCError struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc %s: %s", e.Code, e.Msg)
}

// errToRPC maps typed memory errors to wire error codes.
func errToRPC(err error) *RPCError {
	switch {
	case errors.Is(err, memory.ErrNotFound):
		return &RPCError{Code: "not_found", Msg: err.Error()}
	case errors.Is(err, memory.ErrEmptyBody):
		return &RPCError{Code: "empty_body", Msg: err.Error()}
	case errors.Is(err, memory.ErrDimension):
		return &RPCError{Code: "dimension", Msg: err.Error()}
	default:
		return &RPCError{Code: "internal", Msg: err.Error()}
	}
}

// rpcToErr maps a wire error code back to typed Go errors.
func rpcToErr(e *RPCError) error {
	switch e.Code {
	case "not_found":
		return memory.ErrNotFound
	case "empty_body":
		return memory.ErrEmptyBody
	case "dimension":
		return memory.ErrDimension
	default:
		return errors.New(e.Msg)
	}
}

// ------------------------------------------------------------------ server

// Server exposes a memory.API over a Unix socket.
type Server struct {
	api memory.API
}

// NewServer creates a new Server backed by the provided API implementation.
func NewServer(api memory.API) *Server {
	return &Server{api: api}
}

// Listen removes any stale socket file, creates a new Unix listener at
// SocketPath(cfg), and chmods it to 0600.
func (s *Server) Listen(cfg config.Config) (net.Listener, error) {
	path := SocketPath(cfg)
	// Remove stale socket from a previous run.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", path, err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return ln, nil
}

// Serve accepts connections on ln until ctx is cancelled.  Each connection is
// handled in its own goroutine: one Request decoded, dispatched, one Response
// written, connection closed.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	// Close the listener when the context is done so Accept returns.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// After shutdown the accept error is expected; treat as clean exit.
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("accept: %w", err)
			}
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeResponse(conn, Response{Error: &RPCError{Code: "internal", Msg: "decode request: " + err.Error()}})
		return
	}

	result, rpcErr := s.dispatch(ctx, req)
	if rpcErr != nil {
		writeResponse(conn, Response{Error: rpcErr})
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		writeResponse(conn, Response{Error: &RPCError{Code: "internal", Msg: "marshal result: " + err.Error()}})
		return
	}
	writeResponse(conn, Response{Result: raw})
}

// dispatch routes a request to the appropriate memory.API method.
func (s *Server) dispatch(ctx context.Context, req Request) (any, *RPCError) {
	switch req.Method {
	case "add":
		var p memory.AddRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &RPCError{Code: "internal", Msg: "decode add params: " + err.Error()}
		}
		record, err := s.api.Add(ctx, p)
		if err != nil {
			return nil, errToRPC(err)
		}
		return record, nil

	case "get":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &RPCError{Code: "internal", Msg: "decode get params: " + err.Error()}
		}
		record, err := s.api.Get(ctx, p.ID)
		if err != nil {
			return nil, errToRPC(err)
		}
		return record, nil

	case "search":
		var p memory.SearchRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &RPCError{Code: "internal", Msg: "decode search params: " + err.Error()}
		}
		results, err := s.api.Search(ctx, p)
		if err != nil {
			return nil, errToRPC(err)
		}
		return results, nil

	case "search_detailed":
		var p memory.SearchRequest
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &RPCError{Code: "internal", Msg: "decode search_detailed params: " + err.Error()}
		}
		searcher, ok := s.api.(memory.DetailedSearcher)
		if !ok {
			return nil, &RPCError{Code: "internal", Msg: "search diagnostics unavailable"}
		}
		response, err := searcher.SearchDetailed(ctx, p)
		if err != nil {
			return nil, errToRPC(err)
		}
		return response, nil

	case "forget":
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &RPCError{Code: "internal", Msg: "decode forget params: " + err.Error()}
		}
		if err := s.api.Forget(ctx, p.ID); err != nil {
			return nil, errToRPC(err)
		}
		return struct{}{}, nil

	case "list":
		records, err := s.api.List(ctx)
		if err != nil {
			return nil, errToRPC(err)
		}
		return records, nil

	case "status":
		status, err := s.api.Status(ctx)
		if err != nil {
			return nil, errToRPC(err)
		}
		return status, nil

	case "backfill":
		count, err := s.api.Backfill(ctx)
		if err != nil {
			return nil, errToRPC(err)
		}
		return struct {
			Count int `json:"count"`
		}{Count: count}, nil

	case "optimize":
		if err := s.api.Optimize(ctx); err != nil {
			return nil, errToRPC(err)
		}
		return struct{}{}, nil

	default:
		return nil, &RPCError{Code: "internal", Msg: "unknown method: " + req.Method}
	}
}

func writeResponse(conn net.Conn, resp Response) {
	_ = json.NewEncoder(conn).Encode(resp)
}

// ------------------------------------------------------------------ client

// Client implements memory.API by forwarding each call to a daemon over the
// Unix socket.  One connection is opened per call; no persistent connection is
// maintained.
type Client struct {
	cfg config.Config
}

// NewClient creates a Client that will dial SocketPath(cfg) on each API call.
func NewClient(cfg config.Config) *Client {
	return &Client{cfg: cfg}
}

// Close is a no-op; the client owns no persistent resource.
func (c *Client) Close() error { return nil }

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	conn, err := net.Dial("unix", SocketPath(c.cfg))
	if err != nil {
		return nil, fmt.Errorf("dial socket: %w", err)
	}
	defer conn.Close()

	req := Request{Method: method, Params: rawParams}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.Error != nil {
		return nil, rpcToErr(resp.Error)
	}
	return resp.Result, nil
}

func (c *Client) Add(ctx context.Context, req memory.AddRequest) (memory.Record, error) {
	raw, err := c.call(ctx, "add", req)
	if err != nil {
		return memory.Record{}, err
	}
	var record memory.Record
	return record, json.Unmarshal(raw, &record)
}

func (c *Client) Get(ctx context.Context, id string) (memory.Record, error) {
	raw, err := c.call(ctx, "get", struct {
		ID string `json:"id"`
	}{ID: id})
	if err != nil {
		return memory.Record{}, err
	}
	var record memory.Record
	return record, json.Unmarshal(raw, &record)
}

func (c *Client) Search(ctx context.Context, req memory.SearchRequest) ([]memory.SearchResult, error) {
	raw, err := c.call(ctx, "search", req)
	if err != nil {
		return nil, err
	}
	var results []memory.SearchResult
	return results, json.Unmarshal(raw, &results)
}

func (c *Client) SearchDetailed(ctx context.Context, req memory.SearchRequest) (memory.SearchResponse, error) {
	raw, err := c.call(ctx, "search_detailed", req)
	if err != nil {
		return memory.SearchResponse{}, err
	}
	var response memory.SearchResponse
	return response, json.Unmarshal(raw, &response)
}

func (c *Client) Forget(ctx context.Context, id string) error {
	_, err := c.call(ctx, "forget", struct {
		ID string `json:"id"`
	}{ID: id})
	return err
}

func (c *Client) List(ctx context.Context) ([]memory.Record, error) {
	raw, err := c.call(ctx, "list", struct{}{})
	if err != nil {
		return nil, err
	}
	var records []memory.Record
	return records, json.Unmarshal(raw, &records)
}

func (c *Client) Status(ctx context.Context) (memory.Status, error) {
	raw, err := c.call(ctx, "status", struct{}{})
	if err != nil {
		return memory.Status{}, err
	}
	var status memory.Status
	return status, json.Unmarshal(raw, &status)
}

func (c *Client) Backfill(ctx context.Context) (int, error) {
	raw, err := c.call(ctx, "backfill", struct{}{})
	if err != nil {
		return 0, err
	}
	var result struct {
		Count int `json:"count"`
	}
	return result.Count, json.Unmarshal(raw, &result)
}

func (c *Client) Optimize(ctx context.Context) error {
	_, err := c.call(ctx, "optimize", struct{}{})
	return err
}

// compile-time assertion: *Client must satisfy memory.API.
var _ memory.API = (*Client)(nil)

// compile-time assertion: *Client must satisfy memory.DetailedSearcher.
var _ memory.DetailedSearcher = (*Client)(nil)
