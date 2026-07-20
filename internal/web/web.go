// Package web serves the public landing page and connect API for the hosted service.
package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kidandcat/mercadona-mcp/internal/accounts"
	"github.com/kidandcat/mercadona-mcp/internal/client"
	"github.com/kidandcat/mercadona-mcp/internal/mcp"
	"github.com/kidandcat/mercadona-mcp/internal/oauth"
	"github.com/kidandcat/mercadona-mcp/internal/service"
	"github.com/kidandcat/mercadona-mcp/internal/tools"
)

// Server is the hosted multi-tenant HTTP service.
type Server struct {
	db       *sql.DB
	accounts *accounts.Store
	oauth    *oauth.Service
	baseURL  string

	// Simple IP rate limit for /api/connect.
	mu       sync.Mutex
	connects map[string][]time.Time
}

// New builds the web+MCP server. oauthSvc may be nil only in tests.
func New(db *sql.DB, acc *accounts.Store, oauthSvc *oauth.Service, baseURL string) *Server {
	return &Server{
		db:       db,
		accounts: acc,
		oauth:    oauthSvc,
		baseURL:  strings.TrimRight(baseURL, "/"),
		connects: make(map[string][]time.Time),
	}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if s.oauth != nil {
		s.oauth.Mount(mux)
	}
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/api/connect", s.handleConnect)
	mux.HandleFunc("/api/disconnect", s.handleDisconnect)
	mux.HandleFunc("/api/postal", s.handlePostal)
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.HandleFunc("/mcp/", s.handleMCP)
	return withLog(mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=120")
	_, _ = w.Write([]byte(indexHTML))
}

type connectReq struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	PostalCode string `json:"postal_code"`
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.allowConnect(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts — try again in a few minutes"})
		return
	}
	var req connectReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	res, err := s.accounts.Connect(ctx, req.Email, req.Password, req.PostalCode)
	if err != nil {
		log.Printf("connect: %v", err)
		// Don't leak internal details; surface a clean message.
		msg := "could not connect to Mercadona — check email, password and postal code"
		if strings.Contains(strings.ToLower(err.Error()), "login") {
			msg = "Mercadona login failed — check email and password"
		} else if strings.Contains(strings.ToLower(err.Error()), "postal") {
			msg = "invalid postal code or Mercadona does not deliver there"
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := mcp.BearerToken(r)
	if token == "" {
		var body struct {
			APIToken string `json:"api_token"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
		token = body.APIToken
	}
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_token required"})
		return
	}
	id, err := s.accounts.LookupByToken(r.Context(), token)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}
	if err := s.accounts.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (s *Server) handlePostal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pc := strings.TrimSpace(r.URL.Query().Get("code"))
	if pc == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := client.New().ResolvePostalCode(ctx, pc)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid postal code"})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	// CORS preflight before auth.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Mcp-Session-Id")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, DELETE")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	token := mcp.BearerToken(r)
	if token == "" {
		s.unauthorized(w)
		return
	}

	// Prefer OAuth tokens; fall back to legacy manual API tokens from the website.
	accountID, err := s.resolveAccount(r.Context(), token)
	if err != nil {
		s.unauthorized(w)
		return
	}

	// Per-request service scoped to this account (aliases + tokens isolated).
	svc := service.New(s.db, accountID).WithAccountStore(s.accounts)
	srv := mcp.NewServer("mercadona-mcp", "0.3.0")
	tools.Register(srv, svc)
	srv.HandleHTTP(w, r, nil) // already authenticated above
}

func (s *Server) resolveAccount(ctx context.Context, token string) (string, error) {
	if s.oauth != nil {
		if id, err := s.oauth.LookupAccount(ctx, token); err == nil {
			return id, nil
		}
	}
	return s.accounts.LookupByToken(ctx, token)
}

func (s *Server) unauthorized(w http.ResponseWriter) {
	if s.oauth != nil {
		w.Header().Set("WWW-Authenticate", s.oauth.WWWAuthenticate())
	} else {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
	}
	http.Error(w, "unauthorized — authorize via OAuth (add only the MCP URL) or paste a Bearer token from the website", http.StatusUnauthorized)
}

func (s *Server) allowConnect(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	window := now.Add(-15 * time.Minute)
	list := s.connects[ip]
	kept := list[:0]
	for _, t := range list {
		if t.After(window) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 10 {
		s.connects[ip] = kept
		return false
	}
	s.connects[ip] = append(kept, now)
	return true
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		// Skip noisy health checks.
		if r.URL.Path == "/healthz" {
			return
		}
		log.Printf("%s %s %d %dms", r.Method, r.URL.Path, rec.status, time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(s int) {
	r.status = s
	r.ResponseWriter.WriteHeader(s)
}
