// Package tools registers the Mercadona cart MCP tools.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kidandcat/mercadona-mcp/internal/mcp"
	"github.com/kidandcat/mercadona-mcp/internal/service"
)

// Register attaches all mercadona_* tools to the MCP server.
func Register(s *mcp.Server, svc *service.Service) {
	s.Register(mcp.Tool{
		Name: "mercadona_search",
		Description: "Search Mercadona products by free text (Algolia catalog). " +
			"Returns product id, name, brand, packaging and price. No auth required. " +
			"Use mercadona_add or mercadona_add_by_id to put items in the cart.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search query, e.g. \"leche de avena\""},
				"limit": map[string]any{"type": "integer", "description": "Max hits (default 5, max 20)"},
			},
			"required": []string{"query"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		q, _ := args["query"].(string)
		limit := int(numArg(args, "limit", 5))
		if limit > 20 {
			limit = 20
		}
		hits, err := svc.Search(ctx, q, limit)
		if err != nil {
			return "", err
		}
		return marshal(hits)
	})

	s.Register(mcp.Tool{
		Name: "mercadona_add",
		Description: "Add an item to the Mercadona online cart by free-text name. " +
			"If the query is ambiguous, returns status=\"asked\" with options and a pending_id — " +
			"then call mercadona_resolve with the chosen product_id. " +
			"status=\"added\" means it was added directly (alias hit or single clear match). " +
			"status=\"not_found\" / \"unavailable\" mean nothing was added.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text":     map[string]any{"type": "string", "description": "Product name, e.g. \"huevos L\""},
				"quantity": map[string]any{"type": "number", "description": "Quantity to add (default 1)"},
			},
			"required": []string{"text"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		text, _ := args["text"].(string)
		qty := numArg(args, "quantity", 1)
		res, err := svc.Add(ctx, text, qty)
		if err != nil {
			return "", err
		}
		return marshal(res)
	})

	s.Register(mcp.Tool{
		Name: "mercadona_add_by_id",
		Description: "Add a known Mercadona product_id to the cart (skips search). " +
			"Use after mercadona_search when you already know the id.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"product_id": map[string]any{"type": "string", "description": "Mercadona product id, e.g. \"10379\""},
				"quantity":   map[string]any{"type": "number", "description": "Quantity (default 1)"},
			},
			"required": []string{"product_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id, _ := args["product_id"].(string)
		qty := numArg(args, "quantity", 1)
		res, err := svc.AddByID(ctx, id, qty)
		if err != nil {
			return "", err
		}
		return marshal(res)
	})

	s.Register(mcp.Tool{
		Name: "mercadona_resolve",
		Description: "Resolve a pending ambiguous mercadona_add by selecting one option. " +
			"Pass product_id=\"\" to skip without adding anything.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pending_id": map[string]any{"type": "integer", "description": "pending_id from mercadona_add"},
				"product_id": map[string]any{"type": "string", "description": "Chosen product id, or empty to skip"},
			},
			"required": []string{"pending_id", "product_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		pid := int64(numArg(args, "pending_id", 0))
		if pid <= 0 {
			return "", fmt.Errorf("pending_id required")
		}
		productID, _ := args["product_id"].(string)
		product, cart, err := svc.Resolve(ctx, pid, productID)
		if err != nil {
			return "", err
		}
		if product == nil {
			return marshal(map[string]any{"status": "skipped"})
		}
		return marshal(map[string]any{
			"status":     "added",
			"product":    product,
			"cart_total": cart.Total,
		})
	})

	s.Register(mcp.Tool{
		Name:        "mercadona_remove",
		Description: "Remove the cart line whose display name contains `text` (case-insensitive substring). Errors on no match or multiple matches.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		text, _ := args["text"].(string)
		removed, err := svc.Remove(ctx, text)
		if err != nil {
			return "", err
		}
		return marshal(map[string]any{"status": "removed", "product": removed})
	})

	s.Register(mcp.Tool{
		Name:        "mercadona_list",
		Description: "List current Mercadona cart lines and total.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		cart, err := svc.GetCart(ctx)
		if err != nil {
			return "", err
		}
		return marshal(cart)
	})

	s.Register(mcp.Tool{
		Name:        "mercadona_clear",
		Description: "Empty the Mercadona cart completely.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		if err := svc.Clear(ctx); err != nil {
			return "", err
		}
		return "cart cleared", nil
	})

	s.Register(mcp.Tool{
		Name:        "mercadona_aliases_list",
		Description: "List saved aliases (free-text → Mercadona product_id) learned from past adds.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		aliases, err := svc.ListAliases(ctx)
		if err != nil {
			return "", err
		}
		return marshal(aliases)
	})

	s.Register(mcp.Tool{
		Name:        "mercadona_alias_delete",
		Description: "Delete a saved alias by id (from mercadona_aliases_list).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "integer"},
			},
			"required": []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := int64(numArg(args, "id", 0))
		if id <= 0 {
			return "", fmt.Errorf("id required")
		}
		if err := svc.DeleteAlias(ctx, id); err != nil {
			return "", err
		}
		return "deleted", nil
	})
}

func numArg(args map[string]any, key string, def float64) float64 {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		var f float64
		if _, err := fmt.Sscanf(x, "%f", &f); err == nil {
			return f
		}
	}
	return def
}

func marshal(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
