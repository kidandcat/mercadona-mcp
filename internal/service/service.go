// Package service is the stateful façade used by the MCP tools: session
// persistence, ambiguity resolution, alias bookkeeping, and cart mutations.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kidandcat/mercadona-mcp/internal/client"
)

// Service owns the Mercadona client + local SQLite state.
type Service struct {
	db     *sql.DB
	client *client.Client
}

// New wraps db with a Mercadona client.
func New(db *sql.DB) *Service {
	return &Service{db: db, client: client.New()}
}

// AddResult is the structured result for mercadona_add.
type AddResult struct {
	Status    string           `json:"status"` // "added" | "asked" | "not_found" | "unavailable"
	Product   *client.Product  `json:"product,omitempty"`
	Quantity  float64          `json:"quantity,omitempty"`
	CartTotal float64          `json:"cart_total,omitempty"`
	PendingID int64            `json:"pending_id,omitempty"`
	Options   []client.Product `json:"options,omitempty"`
	AliasText string           `json:"alias_text,omitempty"`
	Message   string           `json:"message,omitempty"`
}

// Alias is a saved free-text → product mapping.
type Alias struct {
	ID          int64  `json:"id"`
	Alias       string `json:"alias"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	UseCount    int    `json:"use_count"`
}

func (s *Service) getSession(ctx context.Context, forceRefresh bool) (*client.Session, error) {
	if !forceRefresh {
		var sess client.Session
		err := s.db.QueryRowContext(ctx, `
			SELECT access_token, COALESCE(refresh_token,''), customer_id
			FROM mercadona_session WHERE id = 1
		`).Scan(&sess.AccessToken, &sess.RefreshToken, &sess.CustomerID)
		if err == nil {
			return &sess, nil
		}
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("session load: %w", err)
		}
	}
	return s.authenticate(ctx)
}

// authenticate obtains a session using (in order):
//  1. MERCADONA_REFRESH_TOKEN (preferred, headless)
//  2. MERCADONA_ACCESS_TOKEN + MERCADONA_CUSTOMER_ID (one-shot, no refresh)
//  3. MERCADONA_USER + MERCADONA_PASS (password login; may require captcha in some cases)
func (s *Service) authenticate(ctx context.Context) (*client.Session, error) {
	if rt := strings.TrimSpace(os.Getenv("MERCADONA_REFRESH_TOKEN")); rt != "" {
		sess, err := s.client.Refresh(ctx, rt)
		if err != nil {
			return nil, fmt.Errorf("refresh token: %w", err)
		}
		// Keep the env refresh if the API didn't rotate one back.
		if sess.RefreshToken == "" {
			sess.RefreshToken = rt
		}
		if err := s.saveSession(ctx, sess); err != nil {
			return nil, err
		}
		return sess, nil
	}

	access := strings.TrimSpace(os.Getenv("MERCADONA_ACCESS_TOKEN"))
	customer := strings.TrimSpace(os.Getenv("MERCADONA_CUSTOMER_ID"))
	if access != "" && customer != "" {
		sess := &client.Session{
			AccessToken:  access,
			RefreshToken: strings.TrimSpace(os.Getenv("MERCADONA_REFRESH_TOKEN")),
			CustomerID:   customer,
		}
		if err := s.saveSession(ctx, sess); err != nil {
			return nil, err
		}
		return sess, nil
	}

	user := strings.TrimSpace(os.Getenv("MERCADONA_USER"))
	pass := os.Getenv("MERCADONA_PASS")
	if user == "" || pass == "" {
		return nil, fmt.Errorf("no credentials: set MERCADONA_REFRESH_TOKEN, or MERCADONA_ACCESS_TOKEN+MERCADONA_CUSTOMER_ID, or MERCADONA_USER+MERCADONA_PASS")
	}
	sess, err := s.client.Login(ctx, user, pass)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	if err := s.saveSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Service) saveSession(ctx context.Context, sess *client.Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mercadona_session (id, access_token, refresh_token, customer_id, updated_at)
		VALUES (1, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			access_token = excluded.access_token,
			refresh_token = excluded.refresh_token,
			customer_id = excluded.customer_id,
			updated_at = CURRENT_TIMESTAMP
	`, sess.AccessToken, sess.RefreshToken, sess.CustomerID)
	if err != nil {
		return fmt.Errorf("session save: %w", err)
	}
	return nil
}

func withRetry[T any](s *Service, ctx context.Context, op func(*client.Session) (T, error)) (T, error) {
	var zero T
	sess, err := s.getSession(ctx, false)
	if err != nil {
		return zero, err
	}
	out, err := op(sess)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, client.ErrUnauthorized) {
		return zero, err
	}
	// Prefer refresh_token over full re-login.
	if sess.RefreshToken != "" {
		refreshed, rerr := s.client.Refresh(ctx, sess.RefreshToken)
		if rerr == nil {
			if refreshed.RefreshToken == "" {
				refreshed.RefreshToken = sess.RefreshToken
			}
			if serr := s.saveSession(ctx, refreshed); serr != nil {
				return zero, serr
			}
			return op(refreshed)
		}
	}
	sess, err = s.getSession(ctx, true)
	if err != nil {
		return zero, err
	}
	return op(sess)
}

// Search products by free text (no auth required for the index itself).
func (s *Service) Search(ctx context.Context, query string, limit int) ([]client.Product, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query required")
	}
	if limit <= 0 {
		limit = 5
	}
	return s.client.Search(ctx, query, limit)
}

// GetCart returns the current cart.
func (s *Service) GetCart(ctx context.Context) (*client.Cart, error) {
	return withRetry(s, ctx, func(sess *client.Session) (*client.Cart, error) {
		return s.client.GetCart(ctx, sess)
	})
}

// Add adds text (qty) to the cart. See AddResult.Status for outcomes.
func (s *Service) Add(ctx context.Context, text string, qty float64) (*AddResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("text required")
	}
	if qty <= 0 {
		qty = 1
	}
	lowered := strings.ToLower(text)

	// 1) exact alias hit
	var aliasID, aliasName string
	err := s.db.QueryRowContext(ctx, `
		SELECT product_id, product_name FROM grocery_aliases WHERE alias = ?
	`, lowered).Scan(&aliasID, &aliasName)
	if err == nil {
		avail, aerr := s.productAvailable(ctx, aliasID)
		if aerr != nil {
			return nil, aerr
		}
		if avail {
			product := client.Product{ID: aliasID, DisplayName: aliasName}
			cart, err := s.addLine(ctx, product, qty)
			if err != nil {
				return nil, err
			}
			_, _ = s.db.ExecContext(ctx, `
				UPDATE grocery_aliases SET use_count = use_count + 1, last_used = CURRENT_TIMESTAMP WHERE alias = ?
			`, lowered)
			for _, l := range cart.Lines {
				if l.Product.ID == aliasID {
					p := l.Product
					return &AddResult{Status: "added", Product: &p, Quantity: l.Quantity, CartTotal: cart.Total}, nil
				}
			}
			return &AddResult{Status: "added", Product: &product, Quantity: qty, CartTotal: cart.Total}, nil
		}
		// Stale alias — drop and fall through to search.
		_, _ = s.db.ExecContext(ctx, `DELETE FROM grocery_aliases WHERE alias = ?`, lowered)
	} else if err != sql.ErrNoRows {
		return nil, fmt.Errorf("alias lookup: %w", err)
	}

	// 2) Algolia search
	hits, err := s.client.Search(ctx, text, 5)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return &AddResult{Status: "not_found", AliasText: text, Message: "no products matched"}, nil
	}

	// 3) single unambiguous hit
	if len(hits) == 1 && strings.Contains(strings.ToLower(hits[0].DisplayName), lowered) {
		hit := hits[0]
		avail, aerr := s.productAvailable(ctx, hit.ID)
		if aerr != nil {
			return nil, aerr
		}
		if !avail {
			return &AddResult{Status: "unavailable", Product: &hit, AliasText: text, Message: "product not available in your zone"}, nil
		}
		cart, err := s.addLine(ctx, hit, qty)
		if err != nil {
			return nil, err
		}
		if err := s.upsertAlias(ctx, lowered, hit.ID, hit.DisplayName); err != nil {
			return nil, err
		}
		return &AddResult{Status: "added", Product: &hit, Quantity: qty, CartTotal: cart.Total}, nil
	}

	// 4) ambiguous → pending
	optsJSON, _ := json.Marshal(hits)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO grocery_pending (alias_text, options_json) VALUES (?, ?)
	`, lowered, string(optsJSON))
	if err != nil {
		return nil, fmt.Errorf("save pending: %w", err)
	}
	pendingID, _ := res.LastInsertId()
	return &AddResult{
		Status:    "asked",
		PendingID: pendingID,
		Options:   hits,
		AliasText: lowered,
		Message:   "ambiguous — pick a product_id and call mercadona_resolve",
	}, nil
}

// AddByID adds a known product id to the cart (skips search/alias).
func (s *Service) AddByID(ctx context.Context, productID string, qty float64) (*AddResult, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return nil, fmt.Errorf("product_id required")
	}
	if qty <= 0 {
		qty = 1
	}
	avail, err := s.productAvailable(ctx, productID)
	if err != nil {
		return nil, err
	}
	if !avail {
		return &AddResult{Status: "unavailable", Product: &client.Product{ID: productID}, Message: "product not available in your zone"}, nil
	}
	product := client.Product{ID: productID, DisplayName: productID}
	cart, err := s.addLine(ctx, product, qty)
	if err != nil {
		return nil, err
	}
	for _, l := range cart.Lines {
		if l.Product.ID == productID {
			p := l.Product
			return &AddResult{Status: "added", Product: &p, Quantity: l.Quantity, CartTotal: cart.Total}, nil
		}
	}
	return &AddResult{Status: "added", Product: &product, Quantity: qty, CartTotal: cart.Total}, nil
}

// Resolve completes a previously ambiguous Add by selecting one of the options.
// Pass productID="" to skip without adding.
func (s *Service) Resolve(ctx context.Context, pendingID int64, productID string) (*client.Product, *client.Cart, error) {
	var (
		aliasText  string
		optsJSON   string
		resolvedAt sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT alias_text, options_json, resolved_at FROM grocery_pending WHERE id = ?
	`, pendingID).Scan(&aliasText, &optsJSON, &resolvedAt)
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("pending %d not found", pendingID)
	}
	if err != nil {
		return nil, nil, err
	}
	if resolvedAt.Valid {
		return nil, nil, fmt.Errorf("pending %d already resolved", pendingID)
	}
	var options []client.Product
	if err := json.Unmarshal([]byte(optsJSON), &options); err != nil {
		return nil, nil, fmt.Errorf("decode options: %w", err)
	}
	if productID == "" {
		_, _ = s.db.ExecContext(ctx, `UPDATE grocery_pending SET resolved_at = CURRENT_TIMESTAMP WHERE id = ?`, pendingID)
		return nil, nil, nil
	}
	var chosen *client.Product
	for i := range options {
		if options[i].ID == productID {
			chosen = &options[i]
			break
		}
	}
	if chosen == nil {
		return nil, nil, fmt.Errorf("product_id %s not among pending options", productID)
	}
	avail, aerr := s.productAvailable(ctx, chosen.ID)
	if aerr != nil {
		return nil, nil, aerr
	}
	if !avail {
		return chosen, nil, fmt.Errorf("%q not available in your zone: %w", chosen.DisplayName, client.ErrProductUnavailable)
	}
	cart, err := s.addLine(ctx, *chosen, 1)
	if err != nil {
		return nil, nil, err
	}
	if err := s.upsertAlias(ctx, aliasText, chosen.ID, chosen.DisplayName); err != nil {
		return nil, nil, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE grocery_pending SET resolved_at = CURRENT_TIMESTAMP WHERE id = ?`, pendingID)
	return chosen, cart, nil
}

// Remove deletes the cart line whose display name matches text (substring).
func (s *Service) Remove(ctx context.Context, text string) (*client.Product, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("text required")
	}
	lowered := strings.ToLower(text)
	cart, err := s.GetCart(ctx)
	if err != nil {
		return nil, err
	}
	var matches []int
	for i, l := range cart.Lines {
		if strings.Contains(strings.ToLower(l.Product.DisplayName), lowered) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no cart line matches %q", text)
	case 1:
	default:
		names := make([]string, 0, len(matches))
		for _, idx := range matches {
			names = append(names, cart.Lines[idx].Product.DisplayName)
		}
		return nil, fmt.Errorf("%d cart lines match %q: %s", len(matches), text, strings.Join(names, "; "))
	}
	idx := matches[0]
	removed := cart.Lines[idx].Product
	cart.Lines = append(cart.Lines[:idx], cart.Lines[idx+1:]...)
	_, err = withRetry(s, ctx, func(sess *client.Session) (*client.Cart, error) {
		return s.client.UpdateCart(ctx, sess, cart)
	})
	if err != nil {
		return nil, err
	}
	return &removed, nil
}

// Clear empties the cart.
func (s *Service) Clear(ctx context.Context) error {
	cart, err := s.GetCart(ctx)
	if err != nil {
		return err
	}
	cart.Lines = nil
	_, err = withRetry(s, ctx, func(sess *client.Session) (*client.Cart, error) {
		return s.client.UpdateCart(ctx, sess, cart)
	})
	return err
}

// ListAliases returns saved aliases ordered by use.
func (s *Service) ListAliases(ctx context.Context) ([]Alias, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, alias, product_id, product_name, use_count
		FROM grocery_aliases ORDER BY use_count DESC, last_used DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alias
	for rows.Next() {
		var a Alias
		if err := rows.Scan(&a.ID, &a.Alias, &a.ProductID, &a.ProductName, &a.UseCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAlias removes one alias by id.
func (s *Service) DeleteAlias(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM grocery_aliases WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("alias %d not found", id)
	}
	return nil
}

// FormatCart renders a cart as a human-readable multi-line string.
func FormatCart(cart *client.Cart) string {
	if cart == nil || len(cart.Lines) == 0 {
		return "(empty cart)"
	}
	var b strings.Builder
	for _, l := range cart.Lines {
		qty := int(l.Quantity)
		if l.Product.Packaging != "" {
			fmt.Fprintf(&b, "- %dx %s (%s) — %.2f€\n", qty, l.Product.DisplayName, l.Product.Packaging, l.Product.UnitPrice)
		} else {
			fmt.Fprintf(&b, "- %dx %s — %.2f€\n", qty, l.Product.DisplayName, l.Product.UnitPrice)
		}
	}
	fmt.Fprintf(&b, "\nTotal: %.2f€", cart.Total)
	return b.String()
}

func (s *Service) productAvailable(ctx context.Context, productID string) (bool, error) {
	return withRetry(s, ctx, func(sess *client.Session) (bool, error) {
		return s.client.CheckAvailability(ctx, sess, productID)
	})
}

func (s *Service) addLine(ctx context.Context, product client.Product, qty float64) (*client.Cart, error) {
	cart, err := s.GetCart(ctx)
	if err != nil {
		return nil, err
	}
	found := false
	for i := range cart.Lines {
		if cart.Lines[i].Product.ID == product.ID {
			cart.Lines[i].Quantity += qty
			found = true
			break
		}
	}
	if !found {
		cart.Lines = append(cart.Lines, client.CartLine{Quantity: qty, Product: product})
	}
	return withRetry(s, ctx, func(sess *client.Session) (*client.Cart, error) {
		return s.client.UpdateCart(ctx, sess, cart)
	})
}

func (s *Service) upsertAlias(ctx context.Context, alias, productID, productName string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO grocery_aliases (alias, product_id, product_name) VALUES (?, ?, ?)
		ON CONFLICT(alias) DO UPDATE SET
			product_id = excluded.product_id,
			product_name = excluded.product_name,
			use_count = grocery_aliases.use_count + 1,
			last_used = CURRENT_TIMESTAMP
	`, alias, productID, productName)
	if err != nil {
		return fmt.Errorf("upsert alias: %w", err)
	}
	return nil
}
