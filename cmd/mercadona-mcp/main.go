// Command mercadona-mcp is an unofficial Model Context Protocol (MCP) server
// that lets AI agents search Mercadona products and manage the online cart.
//
// Transport: stdio (JSON-RPC newline-delimited). Configure it in Claude Desktop,
// Grok Build, Cursor, etc. as a local command.
//
// Auth (first match wins on session miss / refresh failure):
//
//	MERCADONA_REFRESH_TOKEN
//	MERCADONA_ACCESS_TOKEN + MERCADONA_CUSTOMER_ID
//	MERCADONA_USER + MERCADONA_PASS
//
// Optional: DATABASE_PATH (default ~/.mercadona-mcp/data.db), .env next to cwd.
package main

import (
	"bufio"
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kidandcat/mercadona-mcp/internal/mcp"
	"github.com/kidandcat/mercadona-mcp/internal/service"
	"github.com/kidandcat/mercadona-mcp/internal/store"
	"github.com/kidandcat/mercadona-mcp/internal/tools"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("mercadona-mcp: ")

	// Load .env from cwd and from the binary's directory (if present).
	_ = loadDotEnv(".env")
	if exe, err := os.Executable(); err == nil {
		_ = loadDotEnv(filepath.Join(filepath.Dir(exe), ".env"))
	}
	// Also try the project-style home config.
	if home, err := os.UserHomeDir(); err == nil {
		_ = loadDotEnv(filepath.Join(home, ".mercadona-mcp", ".env"))
	}

	db, err := store.Open(os.Getenv("DATABASE_PATH"))
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	svc := service.New(db)
	srv := mcp.NewServer("mercadona-mcp", "0.1.0")
	tools.Register(srv, svc)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.ServeStdio(ctx); err != nil {
		log.Fatalf("stdio: %v", err)
	}
}

// loadDotEnv parses KEY=VALUE lines without overriding existing env vars.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}
		if _, ok := os.LookupEnv(k); ok {
			continue
		}
		_ = os.Setenv(k, v)
	}
	return sc.Err()
}
