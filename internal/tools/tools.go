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
// svc must already be scoped to the correct account (local or hosted).
func Register(s *mcp.Server, svc *service.Service) {
	s.Register(mcp.Tool{
		Name: "mercadona_search",
		Description: "Search Mercadona products by free text (Algolia catalog). " +
			"Returns product id, name, brand, packaging, price, and preferred=true when the " +
			"user has chosen that product before. Preferred hits are listed first. " +
			"Prefer mercadona_add for free-text adds (it auto-picks preferred products). " +
			"If you use mercadona_add_by_id after the user picks one, pass text= the original query so it is remembered.",
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
			"Remembers the user's choice: next time the same text (or a search that includes their " +
			"preferred product as the only preferred hit) is added without asking again. " +
			"If the query is ambiguous, returns status=\"asked\" with options and a pending_id — " +
			"then call mercadona_resolve with the chosen product_id. " +
			"status=\"added\" means it was added directly (alias hit, preferred auto-pick, or single clear match). " +
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
			"Always saves the product as preferred so future mercadona_add / mercadona_search " +
			"can auto-pick it. Pass text= the free-text the user used (e.g. \"leche\") so that " +
			"exact query becomes an alias and next mercadona_add skips the question.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"product_id": map[string]any{"type": "string", "description": "Mercadona product id, e.g. \"10379\""},
				"quantity":   map[string]any{"type": "number", "description": "Quantity (default 1)"},
				"text": map[string]any{
					"type":        "string",
					"description": "Optional free-text query to remember as alias, e.g. the search terms the user said",
				},
			},
			"required": []string{"product_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id, _ := args["product_id"].(string)
		qty := numArg(args, "quantity", 1)
		text, _ := args["text"].(string)
		res, err := svc.AddByID(ctx, id, qty, text)
		if err != nil {
			return "", err
		}
		return marshal(res)
	})

	s.Register(mcp.Tool{
		Name: "mercadona_resolve",
		Description: "Resolve a pending ambiguous mercadona_add by selecting one option. " +
			"Saves the choice as preferred + free-text alias so next time the agent is not asked. " +
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
			"preferred":  true,
			"message":    "saved as preferred for next time",
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
		Name: "mercadona_aliases_list",
		Description: "List saved free-text aliases (exact query → product_id) learned from past adds/resolves.",
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
		Description: "Delete a saved free-text alias by id (from mercadona_aliases_list).",
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

	s.Register(mcp.Tool{
		Name: "mercadona_preferred_list",
		Description: "List preferred products (product_ids the user has chosen before). " +
			"When mercadona_add search results contain exactly one preferred product, it is auto-added.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		prefs, err := svc.ListPreferred(ctx)
		if err != nil {
			return "", err
		}
		return marshal(prefs)
	})

	s.Register(mcp.Tool{
		Name:        "mercadona_preferred_delete",
		Description: "Remove a product from the preferred list by id (from mercadona_preferred_list).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "integer", "description": "Row id from mercadona_preferred_list"},
			},
			"required": []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := int64(numArg(args, "id", 0))
		if id <= 0 {
			return "", fmt.Errorf("id required")
		}
		if err := svc.DeletePreferred(ctx, id); err != nil {
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
