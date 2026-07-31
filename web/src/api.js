export function applyTheme(theme) {
  if (theme === 'system') {
    document.documentElement.removeAttribute('data-theme')
    localStorage.removeItem('sitepass.theme')
    return
  }
  document.documentElement.setAttribute('data-theme', theme)
  localStorage.setItem('sitepass.theme', theme)
}

export function initTheme() {
  const stored = localStorage.getItem('sitepass.theme')
  if (stored === 'light' || stored === 'dark') {
    document.documentElement.setAttribute('data-theme', stored)
  }
}

async function api(path, options = {}) {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      'Accept-Language': document.documentElement.lang || 'en',
      ...(options.body ? { 'Content-Type': 'application/json' } : {}),
      ...options.headers,
    },
    ...options,
  })
  if (res.status === 204) return null
  const data = await res.json()
  if (!res.ok || data?.ok === false) {
    const err = new Error(data?.error?.message || 'request failed')
    err.code = data?.error?.code
    throw err
  }
  return data
}

export async function createToken(projectName) {
  const body = projectName ? JSON.stringify({ project_name: projectName }) : '{}'
  return api('/api/v1/tokens', { method: 'POST', body })
}

export async function fetchStatus(token) {
  return api('/api/v1/status', {
    headers: { Authorization: `Bearer ${token}` },
  })
}

export async function register(username, password) {
  return api('/api/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export async function login(username, password) {
  return api('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export async function logout() {
  return api('/api/v1/auth/logout', { method: 'POST' })
}

export async function fetchMe() {
  return api('/api/v1/auth/me')
}

export async function fetchMyTokens() {
  return api('/api/v1/me/tokens')
}

export function buildAgentInstruction(token, previewUrl, expiresAt, apiBase) {
  const api = (apiBase || (typeof location !== 'undefined' ? location.origin : '')).replace(/\/$/, '')
  let host = 'sitepass.tech'
  try {
    host = new URL(api).hostname
  } catch {
    /* keep default */
  }

  return [
    'Publish this static site build with Sitepass.',
    '',
    `API base (control host — upload HERE only): ${api}`,
    `Control hostname: ${host}`,
    `Upload token: ${token}`,
    `Preview URL (browser only — do NOT POST uploads here): ${previewUrl}`,
    `Expires at: ${expiresAt}`,
    '',
    '1. Build for production.',
    '2. Pack as tar.gz or zip (index.html at archive root, or one nested folder with index.html).',
    '3. Upload raw bytes (Content-Type: application/octet-stream).',
    '',
    '=== Upload (try in this order) ===',
    '',
    'A) Normal upload:',
    `curl -fsS -X POST "${api}/api/v1/upload" \\`,
    `  -H "Authorization: Bearer ${token}" \\`,
    `  -H "Content-Type: application/octet-stream" \\`,
    `  --data-binary @build.tar.gz`,
    '',
    'B) If you get DNS errors (Temporary failure in name resolution, Name or service not known,',
    'getaddrinfo failed, nodename nor servname): DO NOT stop. Bypass system DNS:',
    '',
    'B1) Resolve the control hostname via DNS-over-HTTPS (HTTPS, not UDP/53):',
    `curl -fsS "https://cloudflare-dns.com/dns-query?name=${host}&type=A" -H "accept: application/dns-json"`,
    `curl -fsS "https://dns.google/resolve?name=${host}&type=A"`,
    'Take the first Answer.data IPv4 address as IP.',
    '',
    'B2) Upload with TLS SNI still set to the hostname (required for the certificate):',
    `curl -fsS --resolve ${host}:443:IP -X POST "${api}/api/v1/upload" \\`,
    `  -H "Authorization: Bearer ${token}" \\`,
    `  -H "Content-Type: application/octet-stream" \\`,
    `  --data-binary @build.tar.gz`,
    '',
    'B3) Python equivalent if curl is missing: connect to IP, wrap_socket with server_hostname=',
    `"${host}", Host header ${host}, POST /api/v1/upload with the same headers and body.`,
    '',
    'C) Only if A and B both fail (no outbound HTTPS at all, or IP connect blocked):',
    'give the human the exact curl from A and ask them to run it locally. Do not invent',
    'alternate API hosts. Never POST to the preview hostname.',
    '',
    `Contract: ${api}/llms.txt`,
    'Re-upload with the same token to replace the preview. The preview URL does not change.',
  ].join('\n')
}
