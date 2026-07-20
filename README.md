# mercadona-mcp

Unofficial [Model Context Protocol](https://modelcontextprotocol.io) server for the [Mercadona](https://tienda.mercadona.es) online store.

Two ways to use it:

1. **Hosted (recommended for most people)** — open the website, enter Mercadona email + password + postal code, copy the MCP URL + token into Claude / Grok / Cursor. No install.
2. **Local stdio** — run the binary yourself for full control.

> **Unofficial.** Not affiliated with Mercadona. Talks to the same HTTP endpoints as the website. Bring your own credentials. Use at your own risk. Does **not** place orders.

**Live:** [https://mercadona.cc](https://mercadona.cc)

## Hosted service

1. Go to [mercadona.cc](https://mercadona.cc)
2. Enter your Mercadona account email, password, and delivery postal code
3. Copy the **MCP URL** and **API token**
4. Paste into your AI client:

### Claude Desktop / Claude.ai

```json
{
  "mcpServers": {
    "mercadona": {
      "url": "https://mercadona.cc/mcp",
      "headers": {
        "Authorization": "Bearer YOUR_TOKEN"
      }
    }
  }
}
```

### Grok Build (`~/.grok/config.toml`)

```toml
[mcp_servers.mercadona]
url = "https://mercadona.cc/mcp"
headers = { Authorization = "Bearer YOUR_TOKEN" }
enabled = true
```

### What the AI can do

| Tool | Description |
|------|-------------|
| `mercadona_search` | Search the catalog |
| `mercadona_add` | Add by free-text name (handles ambiguity) |
| `mercadona_add_by_id` | Add a known product id |
| `mercadona_resolve` | Pick one option after an ambiguous add |
| `mercadona_remove` | Remove a cart line by name |
| `mercadona_list` | List cart + total |
| `mercadona_clear` | Empty the cart |
| `mercadona_aliases_list` | Learned name → product aliases |
| `mercadona_alias_delete` | Delete an alias |

The password is **not stored**. Only an encrypted Mercadona session (refresh token) and a hashed API key remain on the server.

## Self-host (`serve`)

```bash
go build -o mercadona-mcp ./cmd/mercadona-mcp

export HTTP_ADDR=127.0.0.1:8086
export PUBLIC_BASE_URL=https://your.domain
export ENCRYPTION_KEY='long-random-secret'
export DATABASE_PATH=./data/mercadona-mcp.db

./mercadona-mcp serve
```

Put a reverse proxy (Caddy/nginx) in front with TLS. Deploy files for systemd + Caddy live under [`deploy/`](deploy/).

## Local stdio (developers)

```bash
go install github.com/kidandcat/mercadona-mcp/cmd/mercadona-mcp@latest
# or: go build -o mercadona-mcp ./cmd/mercadona-mcp
```

Auth via env (or `~/.mercadona-mcp/.env`):

| Priority | Variables |
|----------|-----------|
| 1 | `MERCADONA_REFRESH_TOKEN` |
| 2 | `MERCADONA_ACCESS_TOKEN` + `MERCADONA_CUSTOMER_ID` |
| 3 | `MERCADONA_USER` + `MERCADONA_PASS` |

```toml
# Grok / Claude stdio
[mcp_servers.mercadona]
command = "/path/to/mercadona-mcp"
env = { MERCADONA_USER = "you@example.com", MERCADONA_PASS = "…" }
```

## Privacy & safety

- Hosted: tokens encrypted at rest (AES-GCM), API keys hashed (SHA-256)
- Rate-limited connect endpoint
- No checkout / order placement — cart only
- Disconnect from the website deletes your session from the server

## Related

Logic battle-tested in Minerva (Telegram grocery bot). This repo is the open-source extract + hosted multi-tenant service.

## License

MIT — see [LICENSE](LICENSE).
