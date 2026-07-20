# mercadona-mcp

Unofficial [Model Context Protocol](https://modelcontextprotocol.io) server for the [Mercadona](https://tienda.mercadona.es) online store.

Lets AI agents (Grok, Claude, Cursor, …) **search products** and **manage your shopping cart**: add, remove, list, clear.

> **Unofficial.** Mercadona has no public developer API. This talks to the same HTTP endpoints the website uses. Bring your own credentials, keep a sane request rate, and use at your own risk. Not affiliated with Mercadona.

## Tools

| Tool | Description |
|------|-------------|
| `mercadona_search` | Search the catalog (no login) |
| `mercadona_add` | Add by free-text name (handles ambiguity) |
| `mercadona_add_by_id` | Add a known product id |
| `mercadona_resolve` | Pick one option after an ambiguous add |
| `mercadona_remove` | Remove a cart line by name substring |
| `mercadona_list` | List cart lines + total |
| `mercadona_clear` | Empty the cart |
| `mercadona_aliases_list` | List learned name → product aliases |
| `mercadona_alias_delete` | Delete an alias |

`mercadona_add` flow:

1. Exact saved alias → add.
2. Algolia search; 0 hits → `not_found`.
3. Single clear match → add + save alias.
4. Several hits → `status: "asked"` + `pending_id` + `options` → call `mercadona_resolve`.

Availability is checked against the warehouse bound to your account before committing lines (avoids “not available in your zone” cart junk).

## Install

```bash
go install github.com/kidandcat/mercadona-mcp/cmd/mercadona-mcp@latest
# or
git clone https://github.com/kidandcat/mercadona-mcp
cd mercadona-mcp && go build -o mercadona-mcp ./cmd/mercadona-mcp
```

Requires Go 1.22+.

## Auth

Set credentials via environment variables (or a `.env` file next to the binary / in `~/.mercadona-mcp/.env`):

| Priority | Variables | Notes |
|----------|-----------|--------|
| 1 | `MERCADONA_REFRESH_TOKEN` | Preferred — headless, no captcha |
| 2 | `MERCADONA_ACCESS_TOKEN` + `MERCADONA_CUSTOMER_ID` | One-shot; re-set when expired |
| 3 | `MERCADONA_USER` + `MERCADONA_PASS` | Email/password login (works today; may need captcha later) |

Optional: `DATABASE_PATH` (default `~/.mercadona-mcp/data.db`) for session + aliases.

Session tokens are cached in SQLite. On `401`, the server tries `refresh_token` first, then re-authenticates.

### Getting a refresh token

1. Log in at [tienda.mercadona.es](https://tienda.mercadona.es) in a browser.
2. DevTools → Network → any authenticated `api/customers/...` request, or the `/api/auth/tokens/` response.
3. Copy `refresh_token` from the JSON body (or set `MERCADONA_ACCESS_TOKEN` + `customer_id` for a short session).

## Configure clients

### Grok Build

```toml
# ~/.grok/config.toml
[mcp_servers.mercadona]
command = "/path/to/mercadona-mcp"
env = { MERCADONA_USER = "you@example.com", MERCADONA_PASS = "…" }
# or: MERCADONA_REFRESH_TOKEN = "…"
enabled = true
```

CLI:

```bash
grok mcp add mercadona \
  -e MERCADONA_USER=you@example.com \
  -e MERCADONA_PASS=secret \
  -- /path/to/mercadona-mcp
```

### Claude Desktop / Claude Code

```json
{
  "mcpServers": {
    "mercadona": {
      "command": "/path/to/mercadona-mcp",
      "env": {
        "MERCADONA_USER": "you@example.com",
        "MERCADONA_PASS": "…"
      }
    }
  }
}
```

### Cursor

Same shape under **Settings → MCP** (stdio command + env).

## Example agent flow

```
User: add leche and huevos to the mercadona cart

Agent: mercadona_add text="leche"        → status=asked, options=[…]
Agent: mercadona_resolve pending_id=1 product_id="10379"
Agent: mercadona_add text="huevos L"     → status=added
Agent: mercadona_list                    → cart lines + total
```

## Privacy & safety

- Credentials never leave your machine (stdio local process).
- The cart is the **real** Mercadona online cart for that account — same as the website.
- This server does **not** place orders / checkout. Cart only.
- Rate-limit yourself; don’t hammer the API.

## Related

This logic was battle-tested inside [Minerva](https://github.com/kidandcat) (Telegram grocery bot) and a private multi-tool MCP. This repo is the clean, open-source, Mercadona-only extract for any MCP host.

## License

MIT — see [LICENSE](LICENSE).
