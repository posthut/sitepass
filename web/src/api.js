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

export async function createToken(projectName) {
  const body = projectName ? JSON.stringify({ project_name: projectName }) : '{}'
  const res = await fetch('/api/v1/tokens', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Accept-Language': document.documentElement.lang || 'en' },
    body,
  })
  const data = await res.json()
  if (!res.ok || !data.ok) {
    const err = new Error(data?.error?.message || 'request failed')
    err.code = data?.error?.code
    throw err
  }
  return data
}

export async function fetchStatus(token) {
  const res = await fetch('/api/v1/status', {
    headers: { Authorization: `Bearer ${token}` },
  })
  const data = await res.json()
  if (!res.ok || !data.ok) {
    const err = new Error(data?.error?.message || 'request failed')
    err.code = data?.error?.code
    throw err
  }
  return data
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

export function formatCountdown(expiresAt, now = Date.now()) {
  const ms = Math.max(0, new Date(expiresAt).getTime() - now)
  const totalSec = Math.floor(ms / 1000)
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return {
    ms,
    totalSec,
    label: `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`,
    fraction: (() => {
      // unknown original TTL; UI gets expires_in_seconds from create response
      return null
    })(),
  }
}
