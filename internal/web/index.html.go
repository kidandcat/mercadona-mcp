package web

// indexHTML is the public landing + connect UI (Spanish, Mercadona users).
const indexHTML = `<!DOCTYPE html>
<html lang="es">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Mercadona MCP — tu carrito en Claude, Grok y Cursor</title>
<meta name="description" content="Conecta tu cuenta de Mercadona y deja que la IA gestione el carrito de la compra. Sin instalar nada."/>
<style>
  :root {
    --bg: #0f1410;
    --card: #1a221c;
    --border: #2d3b30;
    --text: #eef4ef;
    --muted: #9aab9e;
    --accent: #5cbe6e;
    --accent-dim: #3a8a4a;
    --danger: #e07070;
    --warn: #e0b050;
    --radius: 14px;
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
  }
  .wrap { max-width: 720px; margin: 0 auto; padding: 2.5rem 1.25rem 4rem; }
  header { margin-bottom: 2rem; }
  .badge {
    display: inline-block; font-size: .75rem; letter-spacing: .04em;
    text-transform: uppercase; color: var(--accent);
    border: 1px solid var(--accent-dim); border-radius: 999px;
    padding: .2rem .7rem; margin-bottom: .8rem;
  }
  h1 { font-size: clamp(1.6rem, 4vw, 2.2rem); margin: 0 0 .5rem; font-weight: 700; letter-spacing: -.02em; }
  .lead { color: var(--muted); font-size: 1.05rem; margin: 0; max-width: 36em; }
  .card {
    background: var(--card); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 1.5rem; margin: 1.5rem 0;
  }
  label { display: block; font-size: .85rem; color: var(--muted); margin: .9rem 0 .35rem; }
  input {
    width: 100%; padding: .7rem .85rem; border-radius: 10px;
    border: 1px solid var(--border); background: #0d120e; color: var(--text);
    font-size: 1rem; outline: none;
  }
  input:focus { border-color: var(--accent-dim); box-shadow: 0 0 0 3px rgba(92,190,110,.15); }
  .row { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
  @media (max-width: 540px) { .row { grid-template-columns: 1fr; } }
  button.primary {
    margin-top: 1.25rem; width: 100%; padding: .85rem 1rem;
    border: none; border-radius: 10px; cursor: pointer;
    background: linear-gradient(180deg, var(--accent), var(--accent-dim));
    color: #061008; font-weight: 700; font-size: 1rem;
  }
  button.primary:disabled { opacity: .55; cursor: wait; }
  button.primary:hover:not(:disabled) { filter: brightness(1.06); }
  .hint { font-size: .8rem; color: var(--muted); margin-top: .75rem; }
  .err {
    display: none; margin-top: 1rem; padding: .75rem 1rem;
    background: rgba(224,112,112,.12); border: 1px solid rgba(224,112,112,.35);
    color: #ffc9c9; border-radius: 10px; font-size: .9rem;
  }
  .err.show { display: block; }
  .result { display: none; }
  .result.show { display: block; }
  .ok {
    padding: .75rem 1rem; margin-bottom: 1rem;
    background: rgba(92,190,110,.12); border: 1px solid rgba(92,190,110,.35);
    color: #c8f0d0; border-radius: 10px; font-size: .95rem;
  }
  .token-box {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: .78rem; word-break: break-all;
    background: #0d120e; border: 1px solid var(--border);
    border-radius: 10px; padding: .85rem; margin: .4rem 0 1rem;
  }
  .copy-row { display: flex; gap: .5rem; flex-wrap: wrap; margin-bottom: .5rem; }
  button.ghost {
    background: transparent; border: 1px solid var(--border); color: var(--text);
    border-radius: 8px; padding: .45rem .75rem; cursor: pointer; font-size: .85rem;
  }
  button.ghost:hover { border-color: var(--accent-dim); }
  pre {
    background: #0d120e; border: 1px solid var(--border); border-radius: 10px;
    padding: .85rem; overflow-x: auto; font-size: .75rem; line-height: 1.4;
  }
  h2 { font-size: 1.1rem; margin: 1.4rem 0 .5rem; }
  h3 { font-size: .95rem; margin: 1rem 0 .35rem; color: var(--muted); font-weight: 600; }
  ul { color: var(--muted); padding-left: 1.2rem; }
  li { margin: .35rem 0; }
  footer { margin-top: 2.5rem; color: var(--muted); font-size: .8rem; }
  a { color: var(--accent); }
  .warn { color: var(--warn); font-size: .85rem; margin-top: .5rem; }
  details { margin-top: .75rem; }
  summary { cursor: pointer; color: var(--muted); font-size: .9rem; }
</style>
</head>
<body>
<div class="wrap">
  <header>
    <div class="badge">no oficial · open source</div>
    <h1>Tu carrito de Mercadona, controlado por IA</h1>
    <p class="lead">
      Conecta tu cuenta de Mercadona en un minuto. Te damos un enlace MCP para
      pegar en Claude, Grok, Cursor o ChatGPT. Di “añade leche y huevos” y se
      meten en tu carrito real de tienda.mercadona.es.
    </p>
  </header>

  <section class="card" id="form-card">
    <h2 style="margin-top:0">Conectar cuenta</h2>
    <p class="hint" style="margin-top:0">
      Usamos las mismas credenciales que la web de Mercadona. La contraseña
      <strong>no se guarda</strong>: solo un token de sesión cifrado y un API key
      para el MCP.
    </p>
    <form id="form">
      <label for="email">Email de Mercadona</label>
      <input id="email" name="email" type="email" autocomplete="username" required placeholder="tu@email.com"/>

      <label for="password">Contraseña</label>
      <input id="password" name="password" type="password" autocomplete="current-password" required placeholder="••••••••"/>

      <label for="postal">Código postal de entrega</label>
      <input id="postal" name="postal_code" type="text" inputmode="numeric" pattern="[0-9]{5}" maxlength="5" required placeholder="28013"/>

      <button class="primary" type="submit" id="submit">Conectar y generar MCP</button>
      <p class="hint">Al conectar aceptas que este servicio (no oficial) actúe en tu nombre sobre el carrito online. No hacemos pedidos ni cobra.</p>
      <div class="err" id="err"></div>
    </form>
  </section>

  <section class="card result" id="result">
    <div class="ok">✓ Cuenta conectada. <strong>Copia el token ahora</strong> — no se vuelve a mostrar.</div>
    <h3>URL del MCP</h3>
    <div class="token-box" id="mcp-url"></div>
    <div class="copy-row">
      <button class="ghost" type="button" data-copy="mcp-url">Copiar URL</button>
    </div>

    <h3>API token (Bearer)</h3>
    <div class="token-box" id="api-token"></div>
    <div class="copy-row">
      <button class="ghost" type="button" data-copy="api-token">Copiar token</button>
    </div>
    <p class="warn">Guárdalo como una contraseña. Quien lo tenga puede gestionar tu carrito.</p>

    <h3>Claude Desktop / Claude.ai</h3>
    <pre id="claude-json"></pre>
    <button class="ghost" type="button" data-copy="claude-json">Copiar config Claude</button>

    <h3>Grok Build</h3>
    <pre id="grok-toml"></pre>
    <button class="ghost" type="button" data-copy="grok-toml">Copiar config Grok</button>

    <details>
      <summary>Desconectar esta cuenta</summary>
      <p class="hint">Borra el token y la sesión guardada en este servidor.</p>
      <button class="ghost" type="button" id="disconnect">Desconectar</button>
    </details>
  </section>

  <section class="card">
    <h2 style="margin-top:0">Qué puede hacer</h2>
    <ul>
      <li>Buscar productos del catálogo</li>
      <li>Añadir / quitar del carrito por nombre o id</li>
      <li>Listar el carrito y el total</li>
      <li>Recordar alias (“leche” → tu marca habitual)</li>
    </ul>
    <p class="hint">No realiza el pedido ni el checkout. Tú confirmas en la app o web de Mercadona.</p>
  </section>

  <footer>
    <p>
      Proyecto open source no afiliado a Mercadona S.A. ·
      <a href="https://github.com/kidandcat/mercadona-mcp">GitHub</a>
      · Credenciales cifradas en reposo (AES-GCM) · Rate limit en login
    </p>
  </footer>
</div>
<script>
const form = document.getElementById('form');
const err = document.getElementById('err');
const result = document.getElementById('result');
const submit = document.getElementById('submit');
let lastToken = '';

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  err.classList.remove('show');
  submit.disabled = true;
  submit.textContent = 'Conectando con Mercadona…';
  try {
    const body = {
      email: document.getElementById('email').value.trim(),
      password: document.getElementById('password').value,
      postal_code: document.getElementById('postal').value.trim(),
    };
    const res = await fetch('/api/connect', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Error desconocido');
    lastToken = data.api_token;
    document.getElementById('mcp-url').textContent = data.mcp_url;
    document.getElementById('api-token').textContent = data.api_token;
    document.getElementById('claude-json').textContent = data.claude_config_json;
    document.getElementById('grok-toml').textContent = data.grok_config_toml;
    result.classList.add('show');
    document.getElementById('password').value = '';
    result.scrollIntoView({ behavior: 'smooth', block: 'start' });
  } catch (ex) {
    err.textContent = ex.message || String(ex);
    err.classList.add('show');
  } finally {
    submit.disabled = false;
    submit.textContent = 'Conectar y generar MCP';
  }
});

document.querySelectorAll('[data-copy]').forEach(btn => {
  btn.addEventListener('click', async () => {
    const id = btn.getAttribute('data-copy');
    const text = document.getElementById(id).textContent;
    try {
      await navigator.clipboard.writeText(text);
      const old = btn.textContent;
      btn.textContent = 'Copiado';
      setTimeout(() => btn.textContent = old, 1200);
    } catch (_) {}
  });
});

document.getElementById('disconnect').addEventListener('click', async () => {
  if (!lastToken) return;
  if (!confirm('¿Desconectar y borrar la sesión de este servidor?')) return;
  const res = await fetch('/api/disconnect', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + lastToken },
    body: JSON.stringify({ api_token: lastToken }),
  });
  if (res.ok) {
    result.classList.remove('show');
    lastToken = '';
    alert('Desconectado.');
  } else {
    alert('No se pudo desconectar.');
  }
});
</script>
</body>
</html>
`
