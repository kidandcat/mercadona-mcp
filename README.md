# mercadona-mcp

Unofficial [Model Context Protocol](https://modelcontextprotocol.io) server for the [Mercadona](https://tienda.mercadona.es) online store.

> **Unofficial.** Not affiliated with Mercadona. Does **not** place orders — cart only.

## MCP URL

```
https://mercadona.cc/mcp
```

Add that URL in Claude, Grok, Cursor, etc. On first use the client opens a browser: Mercadona email + password + postal code. OAuth 2.1 + PKCE.

**Live landing:** [https://mercadona.cc](https://mercadona.cc)

## Tools

| Tool | Description |
|------|-------------|
| `mercadona_search` | Search the catalog |
| `mercadona_add` | Add by free-text name |
| `mercadona_add_by_id` | Add a known product id |
| `mercadona_resolve` | Resolve an ambiguous add |
| `mercadona_remove` | Remove a cart line |
| `mercadona_list` | List cart + total |
| `mercadona_clear` | Empty the cart |
| `mercadona_aliases_list` | Learned aliases |
| `mercadona_alias_delete` | Delete an alias |

## Self-host

```bash
go build -o mercadona-mcp ./cmd/mercadona-mcp

export HTTP_ADDR=127.0.0.1:8086
export PUBLIC_BASE_URL=https://your.domain
export ENCRYPTION_KEY='long-random-secret'
export DATABASE_PATH=./data/mercadona-mcp.db

./mercadona-mcp serve
```

Deploy files under [`deploy/`](deploy/).

## Local stdio (developers)

```bash
go install github.com/kidandcat/mercadona-mcp/cmd/mercadona-mcp@latest
```

Auth via `MERCADONA_USER` + `MERCADONA_PASS` (or refresh token). Run without arguments for stdio MCP.

## License

MIT — see [LICENSE](LICENSE).
