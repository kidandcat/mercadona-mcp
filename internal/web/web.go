// Package web serves the public landing page and the hosted multi-tenant MCP endpoint.
package web

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kidandcat/mercadona-mcp/internal/accounts"
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
}

// New builds the web+MCP server. oauthSvc may be nil only in tests.
func New(db *sql.DB, acc *accounts.Store, oauthSvc *oauth.Service, baseURL string) *Server {
	return &Server{
		db:       db,
		accounts: acc,
		oauth:    oauthSvc,
		baseURL:  strings.TrimRight(baseURL, "/"),
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

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
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

	accountID, err := s.resolveAccount(r.Context(), token)
	if err != nil {
		s.unauthorized(w)
		return
	}

	svc := service.New(s.db, accountID).WithAccountStore(s.accounts)
	srv := mcp.NewServer("mercadona-mcp", "0.3.0")
	tools.Register(srv, svc)
	srv.HandleHTTP(w, r, nil)
}

func (s *Server) resolveAccount(ctx context.Context, token string) (string, error) {
	if s.oauth != nil {
		return s.oauth.LookupAccount(ctx, token)
	}
	return s.accounts.LookupByToken(ctx, token)
}

func (s *Server) unauthorized(w http.ResponseWriter) {
	if s.oauth != nil {
		w.Header().Set("WWW-Authenticate", s.oauth.WWWAuthenticate())
	} else {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
	}
	http.Error(w, "unauthorized — add https://mercadona.cc/mcp and complete OAuth in the browser", http.StatusUnauthorized)
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
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
