// Package oauth implements an OAuth 2.1 Authorization Server for remote MCP
// clients (Claude, Grok, Cursor…): Authorization Code + PKCE, Dynamic Client
// Registration, and discovery metadata.
//
// Authorize page collects Mercadona email/password + postal code, creates a
// hosted account, and binds the issued access token to that account.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kidandcat/mercadona-mcp/internal/accounts"
)

const (
	authCodeTTL = 5 * time.Minute
	tokenTTL    = 90 * 24 * time.Hour
)

// Connector logs a user into Mercadona and returns the account id.
type Connector interface {
	Connect(ctx context.Context, email, password, postalCode string) (*accounts.ConnectResult, error)
}

// Service is the authorization server + token verifier.
type Service struct {
	db        *sql.DB
	baseURL   string
	connector Connector
}

// New creates the OAuth service and migrates tables.
func New(db *sql.DB, baseURL string, connector Connector) (*Service, error) {
	s := &Service{db: db, baseURL: strings.TrimRight(baseURL, "/"), connector: connector}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS oauth_clients (
			client_id TEXT PRIMARY KEY,
			client_name TEXT,
			redirect_uris TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS oauth_codes (
			code TEXT PRIMARY KEY,
			client_id TEXT NOT NULL,
			redirect_uri TEXT NOT NULL,
			code_challenge TEXT NOT NULL,
			code_challenge_method TEXT NOT NULL,
			account_id TEXT,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS oauth_tokens (
			token_hash TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			client_id TEXT NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_tokens_account ON oauth_tokens(account_id)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("oauth migrate: %w", err)
		}
	}
	return nil
}

// Mount registers OAuth routes on mux (both /oauth/* and fallback roots).
func (s *Service) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.handleASMetadata)
	mux.HandleFunc("/.well-known/oauth-protected-resource", s.handlePRMetadata)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", s.handlePRMetadata)

	mux.HandleFunc("/oauth/authorize", s.handleAuthorize)
	mux.HandleFunc("/oauth/token", s.handleToken)
	mux.HandleFunc("/oauth/register", s.handleRegister)

	// Spec fallbacks when metadata discovery fails.
	mux.HandleFunc("/authorize", s.handleAuthorize)
	mux.HandleFunc("/token", s.handleToken)
	mux.HandleFunc("/register", s.handleRegister)
}

// LookupAccount resolves a Bearer access token to an account id.
func (s *Service) LookupAccount(ctx context.Context, accessToken string) (string, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return "", fmt.Errorf("missing token")
	}
	var accountID string
	var expiresAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT account_id, expires_at FROM oauth_tokens WHERE token_hash = ?
	`, hashToken(accessToken)).Scan(&accountID, &expiresAt)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("invalid token")
	}
	if err != nil {
		return "", err
	}
	if time.Now().After(expiresAt) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE token_hash = ?`, hashToken(accessToken))
		return "", fmt.Errorf("token expired")
	}
	return accountID, nil
}

// WWWAuthenticate returns the header value for 401 challenges.
func (s *Service) WWWAuthenticate() string {
	return fmt.Sprintf(`Bearer realm="mcp", resource_metadata="%s/.well-known/oauth-protected-resource"`, s.baseURL)
}

func (s *Service) handleASMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicBase(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"mercadona"},
	})
}

func (s *Service) handlePRMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicBase(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{"mercadona"},
	})
}

// Dynamic Client Registration (RFC 7591) — public clients, no secret.
func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		GrantTypes   []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
		TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	if len(body.RedirectURIs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri", "error_description": "redirect_uris required"})
		return
	}
	for _, u := range body.RedirectURIs {
		if !validRedirectURI(u) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri", "error_description": u})
			return
		}
	}
	clientID, err := randomHex(16)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	urisJSON, _ := json.Marshal(body.RedirectURIs)
	name := strings.TrimSpace(body.ClientName)
	if name == "" {
		name = "mcp-client"
	}
	if _, err := s.db.Exec(`INSERT INTO oauth_clients (client_id, client_name, redirect_uris) VALUES (?, ?, ?)`,
		clientID, name, string(urisJSON)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_name":                name,
		"redirect_uris":              body.RedirectURIs,
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"client_id_issued_at":        time.Now().Unix(),
	})
}

func (s *Service) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.renderAuthorize(w, r, "")
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		s.submitAuthorize(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) renderAuthorize(w http.ResponseWriter, r *http.Request, errMsg string) {
	q := r.URL.Query()
	if r.Method == http.MethodPost {
		q = r.PostForm
	}
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	method := q.Get("code_challenge_method")
	if method == "" {
		method = "S256"
	}
	if clientID == "" || redirectURI == "" {
		http.Error(w, "client_id and redirect_uri required", http.StatusBadRequest)
		return
	}
	if codeChallenge == "" || method != "S256" {
		http.Error(w, "PKCE code_challenge with S256 is required", http.StatusBadRequest)
		return
	}
	if !validRedirectURI(redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	// Unknown client_id is OK for soft-open DCR race; we still accept if redirect is safe.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = authorizeTmpl.Execute(w, map[string]any{
		"ClientID":            clientID,
		"RedirectURI":         redirectURI,
		"State":               state,
		"CodeChallenge":       codeChallenge,
		"CodeChallengeMethod": method,
		"Error":               errMsg,
		"Email":               q.Get("email"),
		"PostalCode":          q.Get("postal_code"),
	})
}

func (s *Service) submitAuthorize(w http.ResponseWriter, r *http.Request) {
	form := r.PostForm
	clientID := form.Get("client_id")
	redirectURI := form.Get("redirect_uri")
	state := form.Get("state")
	codeChallenge := form.Get("code_challenge")
	method := form.Get("code_challenge_method")
	if method == "" {
		method = "S256"
	}
	email := form.Get("email")
	password := form.Get("password")
	postal := form.Get("postal_code")

	if clientID == "" || redirectURI == "" || codeChallenge == "" {
		s.renderAuthorize(w, r, "Parámetros OAuth incompletos")
		return
	}
	if !validRedirectURI(redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	res, err := s.connector.Connect(ctx, email, password, postal)
	if err != nil {
		log.Printf("oauth authorize connect: %v", err)
		msg := "No se pudo conectar con Mercadona. Revisa email, contraseña y código postal."
		if strings.Contains(strings.ToLower(err.Error()), "login") {
			msg = "Login de Mercadona fallido — revisa email y contraseña."
		} else if strings.Contains(strings.ToLower(err.Error()), "postal") {
			msg = "Código postal no válido o sin reparto."
		}
		s.renderAuthorize(w, r, msg)
		return
	}

	code, err := randomHex(24)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	expires := time.Now().Add(authCodeTTL)
	if _, err := s.db.Exec(`
		INSERT INTO oauth_codes (code, client_id, redirect_uri, code_challenge, code_challenge_method, account_id, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, code, clientID, redirectURI, codeChallenge, method, res.AccountID, expires); err != nil {
		http.Error(w, "internal: db", http.StatusInternalServerError)
		return
	}

	dst, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	q := dst.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	dst.RawQuery = q.Encode()
	http.Redirect(w, r, dst.String(), http.StatusFound)
}

func (s *Service) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		tokenError(w, "invalid_request", "bad form")
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		tokenError(w, "unsupported_grant_type", "only authorization_code supported")
		return
	}
	code := r.PostForm.Get("code")
	clientID := r.PostForm.Get("client_id")
	verifier := r.PostForm.Get("code_verifier")
	redirectURI := r.PostForm.Get("redirect_uri")
	if code == "" || verifier == "" {
		tokenError(w, "invalid_request", "code and code_verifier required")
		return
	}

	var (
		storedClient    string
		storedRedirect  string
		storedChallenge string
		storedMethod    string
		accountID       sql.NullString
		expiresAt       time.Time
	)
	err := s.db.QueryRow(`
		SELECT client_id, redirect_uri, code_challenge, code_challenge_method, account_id, expires_at
		FROM oauth_codes WHERE code = ?
	`, code).Scan(&storedClient, &storedRedirect, &storedChallenge, &storedMethod, &accountID, &expiresAt)
	if err == sql.ErrNoRows {
		tokenError(w, "invalid_grant", "unknown code")
		return
	}
	if err != nil {
		tokenError(w, "server_error", "db lookup")
		return
	}
	_, _ = s.db.Exec(`DELETE FROM oauth_codes WHERE code = ?`, code)

	if time.Now().After(expiresAt) {
		tokenError(w, "invalid_grant", "code expired")
		return
	}
	if clientID != "" && clientID != storedClient {
		tokenError(w, "invalid_grant", "client_id mismatch")
		return
	}
	if redirectURI != "" && redirectURI != storedRedirect {
		tokenError(w, "invalid_grant", "redirect_uri mismatch")
		return
	}
	if !verifyPKCE(verifier, storedChallenge, storedMethod) {
		tokenError(w, "invalid_grant", "PKCE verification failed")
		return
	}
	if !accountID.Valid || accountID.String == "" {
		tokenError(w, "invalid_grant", "code not bound to account")
		return
	}

	accessToken, err := randomHex(32)
	if err != nil {
		tokenError(w, "server_error", "rand")
		return
	}
	tokenExpires := time.Now().Add(tokenTTL)
	if _, err := s.db.Exec(`
		INSERT INTO oauth_tokens (token_hash, account_id, client_id, expires_at)
		VALUES (?, ?, ?, ?)
	`, hashToken(accessToken), accountID.String, storedClient, tokenExpires); err != nil {
		tokenError(w, "server_error", "db insert")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(tokenTTL.Seconds()),
		"scope":        "mercadona",
	})
}

func verifyPKCE(verifier, challenge, method string) bool {
	if method != "S256" {
		return false
	}
	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) == 1
}

func validRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return true
	case "http":
		host := strings.ToLower(u.Hostname())
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		// Custom schemes used by desktop apps (claude://, cursor://, etc.)
		return len(u.Scheme) >= 2 && !strings.Contains(u.Scheme, ".")
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func tokenError(w http.ResponseWriter, code, desc string) {
	status := http.StatusBadRequest
	if code == "server_error" {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Service) publicBase(r *http.Request) string {
	if s.baseURL != "" {
		return s.baseURL
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return proto + "://" + host
}

var authorizeTmpl = template.Must(template.New("auth").Parse(`<!DOCTYPE html>
<html lang="es">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Conectar Mercadona — MCP</title>
<style>
  :root {
    --bg:#0f1410; --card:#1a221c; --border:#2d3b30; --text:#eef4ef;
    --muted:#9aab9e; --accent:#5cbe6e; --accent-dim:#3a8a4a; --danger:#e07070;
  }
  *{box-sizing:border-box}
  body{margin:0;min-height:100vh;font-family:system-ui,-apple-system,sans-serif;
    background:radial-gradient(900px 500px at 20% -10%,#1e3a24,transparent 55%),var(--bg);
    color:var(--text);display:flex;align-items:center;justify-content:center;padding:1.25rem}
  .card{background:var(--card);border:1px solid var(--border);border-radius:14px;
    padding:1.75rem;max-width:420px;width:100%}
  h1{margin:0 0 .4rem;font-size:1.35rem}
  p{color:var(--muted);font-size:.92rem;margin:0 0 1.2rem;line-height:1.45}
  label{display:block;font-size:.8rem;color:var(--muted);margin:.85rem 0 .3rem}
  input{width:100%;padding:.7rem .85rem;border-radius:10px;border:1px solid var(--border);
    background:#0d120e;color:var(--text);font-size:1rem}
  input:focus{outline:none;border-color:var(--accent-dim);box-shadow:0 0 0 3px rgba(92,190,110,.15)}
  button{width:100%;margin-top:1.2rem;padding:.85rem;border:none;border-radius:10px;
    background:linear-gradient(180deg,var(--accent),var(--accent-dim));color:#061008;
    font-weight:700;font-size:1rem;cursor:pointer}
  button:hover{filter:brightness(1.05)}
  .err{background:rgba(224,112,112,.12);border:1px solid rgba(224,112,112,.35);
    color:#ffc9c9;border-radius:10px;padding:.7rem .85rem;font-size:.88rem;margin-bottom:1rem}
  .meta{font-size:.7rem;color:#5a6a5e;margin-top:1rem;word-break:break-all}
  .hint{font-size:.78rem;color:var(--muted);margin-top:.75rem}
</style>
</head>
<body>
<div class="card">
  <h1>Conectar tu Mercadona</h1>
  <p>Este asistente de IA quiere gestionar el carrito de tu cuenta de Mercadona Online. Introduce tus credenciales y el código postal de entrega.</p>
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  <form method="POST" action="/oauth/authorize">
    <input type="hidden" name="client_id" value="{{.ClientID}}"/>
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}"/>
    <input type="hidden" name="state" value="{{.State}}"/>
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}"/>
    <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}"/>

    <label for="email">Email de Mercadona</label>
    <input id="email" name="email" type="email" autocomplete="username" required value="{{.Email}}" autofocus/>

    <label for="password">Contraseña</label>
    <input id="password" name="password" type="password" autocomplete="current-password" required/>

    <label for="postal_code">Código postal de entrega</label>
    <input id="postal_code" name="postal_code" type="text" inputmode="numeric" pattern="[0-9]{5}" maxlength="5" required value="{{.PostalCode}}" placeholder="28013"/>

    <button type="submit">Autorizar y conectar</button>
  </form>
  <p class="hint">La contraseña no se guarda. Solo una sesión cifrada para el carrito. No se realizan pedidos.</p>
  <div class="meta">cliente: {{.ClientID}}</div>
</div>
</body>
</html>`))
