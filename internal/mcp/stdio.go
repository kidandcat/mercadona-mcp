// Package mcp implements a minimal stdio JSON-RPC transport for the
// Model Context Protocol (protocol version 2024-11-05).
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

const protocolVersion = "2024-11-05"

// JSON-RPC 2.0

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MCP types

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// ToolHandler executes a tool and returns a text result (or error).
type ToolHandler func(ctx context.Context, args map[string]any) (string, error)

type registeredTool struct {
	Tool
	handler ToolHandler
}

// Server is a stdio MCP server with a static tool registry.
type Server struct {
	name    string
	version string
	mu      sync.RWMutex
	tools   map[string]registeredTool
}

// NewServer creates an empty tool registry.
func NewServer(name, version string) *Server {
	if version == "" {
		version = "0.1.0"
	}
	return &Server{
		name:    name,
		version: version,
		tools:   make(map[string]registeredTool),
	}
}

// Register adds a tool.
func (s *Server) Register(t Tool, h ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[t.Name] = registeredTool{Tool: t, handler: h}
}

func (s *Server) listTools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, t.Tool)
	}
	return out
}

// ServeStdio reads newline-delimited JSON-RPC from stdin and writes responses to stdout.
// Logs go to stderr only. Each response is flushed immediately (stdout is block-buffered
// when piped; without Flush the host can hang waiting for the first reply).
func (s *Server) ServeStdio(ctx context.Context) error {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("mercadona-mcp: ")

	// Large tool results (cart dumps) can exceed the default scanner buffer.
	scanner := bufio.NewScanner(os.Stdin)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)

	out := bufio.NewWriter(os.Stdout)
	enc := json.NewEncoder(out)
	write := func(v any) error {
		if err := enc.Encode(v); err != nil {
			return err
		}
		return out.Flush()
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(bytesTrimSpace(line)) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			log.Printf("parse request: %v", err)
			_ = write(errResp(nil, -32700, "parse error: "+err.Error()))
			continue
		}
		if isNotification(req) {
			s.handle(ctx, req)
			continue
		}
		if err := write(s.handle(ctx, req)); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func isNotification(req Request) bool {
	return len(req.ID) == 0 || string(req.ID) == "null"
}

func (s *Server) handle(ctx context.Context, req Request) Response {
	switch req.Method {
	case "initialize":
		return Response{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": s.name, "version": s.version},
			},
		}
	case "notifications/initialized", "notifications/cancelled", "notifications/progress":
		return Response{JSONRPC: "2.0", ID: req.ID}
	case "ping":
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return Response{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{"tools": s.listTools()},
		}
	case "tools/call":
		var p ToolCallParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, -32602, "invalid params: "+err.Error())
		}
		s.mu.RLock()
		t, ok := s.tools[p.Name]
		s.mu.RUnlock()
		if !ok {
			return errResp(req.ID, -32601, "unknown tool: "+p.Name)
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		text, err := t.handler(ctx, p.Arguments)
		if err != nil {
			log.Printf("tool %s: %v", p.Name, err)
			return Response{
				JSONRPC: "2.0", ID: req.ID,
				Result: CallToolResult{
					Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
					IsError: true,
				},
			}
		}
		return Response{
			JSONRPC: "2.0", ID: req.ID,
			Result: CallToolResult{Content: []Content{{Type: "text", Text: text}}},
		}
	default:
		return errResp(req.ID, -32601, "method not found: "+req.Method)
	}
}

func errResp(id json.RawMessage, code int, msg string) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: msg}}
}

func bytesTrimSpace(b []byte) []byte {
	// small helper to avoid importing bytes for one call
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\r' || b[j-1] == '\n') {
		j--
	}
	return b[i:j]
}
