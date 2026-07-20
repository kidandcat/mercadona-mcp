package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
)

// AuthFunc validates a Bearer token and returns a request-scoped value
// (e.g. account id) to stash in the request context, or an error.
// The returned value is ignored by the default handler — use AuthAccount
// middleware separately if you need it. Here we only need pass/fail for
// static single-tenant, but Hosted uses a custom wrapper.
type AuthFunc func(r *http.Request) error

// HandleHTTP serves Streamable HTTP MCP (POST JSON-RPC, single response).
// auth may be nil to skip auth (not recommended for public).
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request, auth AuthFunc) {
	// CORS for browser-based MCP clients / diagnostics.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Mcp-Session-Id")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, DELETE")

	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodGet:
		// No SSE server-push channel.
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	case http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodPost:
		// ok
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if auth != nil {
		if err := auth(r); err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		writeHTTPError(w, nil, -32700, "read body: "+err.Error())
		return
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if strings.HasPrefix(trimmed, "[") {
		var reqs []Request
		if err := json.Unmarshal(raw, &reqs); err != nil {
			writeHTTPError(w, nil, -32700, "parse batch: "+err.Error())
			return
		}
		resps := make([]Response, 0, len(reqs))
		for _, req := range reqs {
			if isNotification(req) {
				s.handle(r.Context(), req)
				continue
			}
			resps = append(resps, s.handle(r.Context(), req))
		}
		if len(resps) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeHTTPJSON(w, resps)
		return
	}
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeHTTPError(w, nil, -32700, "parse request: "+err.Error())
		return
	}
	if isNotification(req) {
		s.handle(r.Context(), req)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeHTTPJSON(w, s.handle(r.Context(), req))
}

func writeHTTPJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil && !errors.Is(err, http.ErrBodyNotAllowed) {
		log.Printf("write response: %v", err)
	}
}

func writeHTTPError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	writeHTTPJSON(w, errResp(id, code, msg))
}

// BearerToken extracts the Bearer token from Authorization header.
func BearerToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}
