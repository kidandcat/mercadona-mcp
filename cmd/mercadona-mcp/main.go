// Command mercadona-mcp is an unofficial Model Context Protocol (MCP) server
// for the Mercadona online store.
//
// Modes:
//
//	mercadona-mcp            stdio MCP (local; env credentials)
//	mercadona-mcp serve      hosted multi-tenant web + HTTP MCP
//
// Local auth (first match wins):
//
//	MERCADONA_REFRESH_TOKEN
//	MERCADONA_ACCESS_TOKEN + MERCADONA_CUSTOMER_ID
//	MERCADONA_USER + MERCADONA_PASS
//
// Hosted (serve):
//
//	HTTP_ADDR        default :8086
//	PUBLIC_BASE_URL  e.g. https://mercadona.example.com
//	ENCRYPTION_KEY   required — passphrase for AES-GCM token encryption
//	DATABASE_PATH    default ./data/mercadona-mcp.db
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kidandcat/mercadona-mcp/internal/accounts"
	"github.com/kidandcat/mercadona-mcp/internal/cryptox"
	"github.com/kidandcat/mercadona-mcp/internal/mcp"
	"github.com/kidandcat/mercadona-mcp/internal/oauth"
	"github.com/kidandcat/mercadona-mcp/internal/service"
	"github.com/kidandcat/mercadona-mcp/internal/store"
	"github.com/kidandcat/mercadona-mcp/internal/tools"
	"github.com/kidandcat/mercadona-mcp/internal/web"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("mercadona-mcp: ")

	loadEnvFiles()

	if len(os.Args) > 1 && os.Args[1] == "serve" {
		if err := runServe(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := runStdio(); err != nil {
		log.Fatal(err)
	}
}

func runStdio() error {
	db, err := store.Open(os.Getenv("DATABASE_PATH"))
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer db.Close()

	svc := service.New(db, store.LocalAccountID)
	srv := mcp.NewServer("mercadona-mcp", "0.2.0")
	tools.Register(srv, svc)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return srv.ServeStdio(ctx)
}

func runServe() error {
	base := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL"))
	if base == "" {
		return fmt.Errorf("PUBLIC_BASE_URL is required (e.g. https://mercadona.example.com)")
	}
	encKey := strings.TrimSpace(os.Getenv("ENCRYPTION_KEY"))
	if encKey == "" {
		return fmt.Errorf("ENCRYPTION_KEY is required (long random passphrase)")
	}
	box, err := cryptox.New(encKey)
	if err != nil {
		return err
	}

	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "./data/mercadona-mcp.db"
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer db.Close()

	acc := accounts.New(db, box, base)
	oauthSvc, err := oauth.New(db, base, acc)
	if err != nil {
		return fmt.Errorf("oauth: %w", err)
	}
	webSrv := web.New(db, acc, oauthSvc, base)

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8086"
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           webSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		shCtx, c := context.WithTimeout(context.Background(), 8*time.Second)
		defer c()
		_ = httpSrv.Shutdown(shCtx)
	}()

	log.Printf("serving on %s (public %s)", addr, base)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func loadEnvFiles() {
	_ = loadDotEnv(".env")
	if exe, err := os.Executable(); err == nil {
		_ = loadDotEnv(filepath.Join(filepath.Dir(exe), ".env"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		_ = loadDotEnv(filepath.Join(home, ".mercadona-mcp", ".env"))
	}
}

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
