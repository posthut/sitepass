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

export async function fetchHealth() {
  return api('/api/v1/health')
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

export function buildAgentInstruction(token, previewUrl, expiresAt) {
  return [
    'Publish this static site build with Sitepass.',
    '',
    `Upload token: ${token}`,
    `Preview URL: ${previewUrl}`,
    `Expires at: ${expiresAt}`,
    '',
    '1. Build the project for production.',
    '2. Pack the build output as tar.gz or zip (directory must contain index.html, or a single nested folder with index.html).',
    '3. Upload:',
    '',
    `curl -X POST "$SITEPASS_API/api/v1/upload" \\`,
    `  -H "Authorization: Bearer ${token}" \\`,
    `  -H "Content-Type: application/octet-stream" \\`,
    `  --data-binary @build.tar.gz`,
    '',
    'Replace $SITEPASS_API with the control site origin (same host as this page).',
    'Read /llms.txt for the full contract and error codes.',
    'Re-upload with the same token to replace the preview. The URL does not change.',
  ].join('\n')
}
