package web

// indexHTML is the public landing page — MCP URL only; auth is OAuth in the client.
const indexHTML = `<!DOCTYPE html>
<html lang="es">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Mercadona MCP</title>
<meta name="description" content="MCP de Mercadona: añade la URL en tu cliente de IA y conecta con OAuth."/>
<style>
  :root {
    --bg: #0f1410;
    --card: #1a221c;
    --border: #2d3b30;
    --text: #eef4ef;
    --muted: #9aab9e;
    --accent: #5cbe6e;
    --accent-dim: #3a8a4a;
    --font: "Segoe UI", system-ui, -apple-system, sans-serif;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; min-height: 100vh;
    font-family: var(--font);
    background:
      radial-gradient(1200px 600px at 10% -10%, #1e3a24 0%, transparent 55%),
      radial-gradient(900px 500px at 100% 0%, #1a2a3a 0%, transparent 50%),
      var(--bg);
    color: var(--text);
    line-height: 1.5;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1.5rem;
  }
  .wrap { max-width: 520px; width: 100%; text-align: center; }
  .badge {
    display: inline-block; font-size: .75rem; letter-spacing: .04em;
    text-transform: uppercase; color: var(--accent);
    border: 1px solid var(--accent-dim); border-radius: 999px;
    padding: .2rem .7rem; margin-bottom: 1rem;
  }
  h1 { font-size: clamp(1.5rem, 4vw, 2rem); margin: 0 0 .6rem; font-weight: 700; letter-spacing: -.02em; }
  .lead { color: var(--muted); font-size: 1.02rem; margin: 0 0 1.75rem; }
  .url-box {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 1rem;
    word-break: break-all;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 14px;
    padding: 1.1rem 1.25rem;
    margin-bottom: 1rem;
  }
  button {
    background: linear-gradient(180deg, var(--accent), var(--accent-dim));
    color: #061008;
    border: none;
    border-radius: 10px;
    padding: .7rem 1.25rem;
    font-weight: 700;
    font-size: .95rem;
    cursor: pointer;
  }
  button:hover { filter: brightness(1.06); }
  button.copied { opacity: .85; }
  .hint { color: var(--muted); font-size: .85rem; margin-top: 1.5rem; }
  footer { margin-top: 2.5rem; color: var(--muted); font-size: .78rem; }
  a { color: var(--accent); }
</style>
</head>
<body>
<div class="wrap">
  <div class="badge">no oficial · open source</div>
  <h1>Mercadona MCP</h1>
  <p class="lead">
    Añade esta URL en Claude, Grok o Cursor. Al conectar se abre el navegador
    para entrar con tu cuenta de Mercadona y el código postal.
  </p>
  <div class="url-box" id="url">https://mercadona.cc/mcp</div>
  <button type="button" id="copy">Copiar URL</button>
  <p class="hint">No se realizan pedidos. Solo carrito.</p>
  <footer>
    <a href="https://github.com/kidandcat/mercadona-mcp">GitHub</a>
  </footer>
</div>
<script>
const btn = document.getElementById('copy');
const url = document.getElementById('url').textContent.trim();
btn.addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText(url);
    btn.textContent = 'Copiado';
    btn.classList.add('copied');
    setTimeout(() => { btn.textContent = 'Copiar URL'; btn.classList.remove('copied'); }, 1200);
  } catch (_) {}
});
</script>
</body>
</html>
`
