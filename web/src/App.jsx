import { useEffect, useMemo, useState } from 'react'
import { applyTheme, buildAgentInstruction, createToken, fetchStatus, initTheme } from './api'
import { detectLanguage, setLanguage, t } from './i18n'
import './app.css'

initTheme()

function StatusPill({ status, label }) {
  return <span className={`pill pill-${status}`}>{label}</span>
}

function ExpiryRule({ fraction, warn }) {
  const scale = Math.max(0, Math.min(1, fraction ?? 0))
  return (
    <div className="expiry-rule" aria-hidden="true">
      <span
        className={`expiry-rule__fill${warn ? ' is-warn' : ''}`}
        style={{ transform: `scaleX(${scale})` }}
      />
    </div>
  )
}

export default function App() {
  const [lang, setLang] = useState(detectLanguage)
  const [projectName, setProjectName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [session, setSession] = useState(null)
  const [status, setStatus] = useState(null)
  const [now, setNow] = useState(Date.now())
  const [copied, setCopied] = useState('')
  const [liveMsg, setLiveMsg] = useState('')

  useEffect(() => {
    setLanguage(lang)
  }, [lang])

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  useEffect(() => {
    if (!session?.token) return undefined
    let cancelled = false
    const tick = async () => {
      try {
        const st = await fetchStatus(session.token)
        if (!cancelled) setStatus(st)
      } catch (err) {
        if (!cancelled && (err.code === 'token_expired' || err.code === 'token_revoked')) {
          setStatus((prev) => ({ ...(prev || {}), expired: true, has_build: false }))
        }
      }
    }
    tick()
    const id = setInterval(tick, 5000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [session])

  const expiresAt = status?.expires_at || session?.expires_at
  const totalTTL = session?.expires_in_seconds || 1800
  const remainingMs = expiresAt ? Math.max(0, new Date(expiresAt).getTime() - now) : 0
  const remainingSec = Math.floor(remainingMs / 1000)
  const fraction = Math.max(0, Math.min(1, remainingMs / (totalTTL * 1000)))
  const warn = remainingSec > 0 && remainingSec <= 300
  const expired = remainingSec <= 0 && !!session
  const live = !expired && !!status?.has_build
  const waiting = !expired && !!session && !status?.has_build

  const countdown = useMemo(() => {
    const m = Math.floor(remainingSec / 60)
    const s = remainingSec % 60
    return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  }, [remainingSec])

  async function onCreate(e) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const data = await createToken(projectName.trim())
      setSession(data)
      setStatus({
        preview_url: data.preview_url,
        expires_at: data.expires_at,
        has_build: false,
        revision: 0,
        upload_count: 0,
      })
    } catch (err) {
      setError(err.message || t(lang, 'error.generic'))
    } finally {
      setBusy(false)
    }
  }

  async function copyText(kind, text) {
    await navigator.clipboard.writeText(text)
    setCopied(kind)
    setLiveMsg(t(lang, 'token.copied'))
    setTimeout(() => setCopied(''), 1500)
  }

  const instruction = session
    ? buildAgentInstruction(session.token, session.preview_url, session.expires_at)
    : ''

  return (
    <main className="page">
      <div className="topbar">
        <div className="brand">{t(lang, 'app.brand')}</div>
        <div className="controls">
          <label className="sr-only" htmlFor="lang">
            {t(lang, 'lang.label')}
          </label>
          <select
            id="lang"
            value={lang}
            onChange={(e) => setLang(e.target.value)}
          >
            <option value="en">EN</option>
            <option value="ru">RU</option>
            <option value="kk">KK</option>
          </select>
          <select
            aria-label="theme"
            defaultValue={localStorage.getItem('sitepass.theme') || 'system'}
            onChange={(e) => applyTheme(e.target.value)}
          >
            <option value="system">{t(lang, 'theme.system')}</option>
            <option value="light">{t(lang, 'theme.light')}</option>
            <option value="dark">{t(lang, 'theme.dark')}</option>
          </select>
        </div>
      </div>

      <h1 className="headline">{t(lang, 'app.headline')}</h1>
      <p className="lead">{t(lang, 'app.lead')}</p>

      {!session || expired ? (
        <form className="card" onSubmit={onCreate}>
          <div className="field">
            <label htmlFor="project">{t(lang, 'token.create.project_name.label')}</label>
            <input
              id="project"
              maxLength={48}
              value={projectName}
              placeholder={t(lang, 'token.create.project_name.placeholder')}
              onChange={(e) => setProjectName(e.target.value)}
            />
            <div className="helper">
              {t(lang, 'token.create.project_name.helper')}
              {projectName.length > 40 ? ` ${projectName.length}/48` : ''}
            </div>
          </div>
          <button className="btn btn-primary" type="submit" disabled={busy}>
            {busy ? '…' : t(lang, 'token.create.button')}
          </button>
          {expired ? <p className="helper" style={{ marginTop: 'var(--space-4)' }}>{t(lang, 'expired.body')}</p> : null}
          {error ? <p className="error" role="alert">{error}</p> : null}
        </form>
      ) : (
        <section className={`card${expired ? ' is-expired' : ''}`}>
          <ExpiryRule fraction={fraction} warn={warn} />
          <div className="row" style={{ marginTop: 'var(--space-3)' }}>
            <span className="label">{t(lang, 'token.label')}</span>
            <span className="mono" aria-live="off">
              {countdown}
            </span>
          </div>
          <p className="mono-lg">{session.token}</p>

          <div className="row">
            <StatusPill
              status={live ? 'live' : waiting ? 'waiting' : 'expired'}
              label={
                live
                  ? t(lang, 'status.live')
                  : waiting
                    ? t(lang, 'status.waiting')
                    : t(lang, 'status.expired')
              }
            />
          </div>
          <p className="helper">{waiting ? t(lang, 'waiting.body') : null}</p>

          <div className="actions">
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => copyText('instruction', instruction)}
            >
              {copied === 'instruction' ? t(lang, 'token.copied') : t(lang, 'token.copy_instruction')}
            </button>
            <button
              type="button"
              className="btn btn-quiet"
              onClick={() => copyText('token', session.token)}
            >
              {copied === 'token' ? t(lang, 'token.copied') : t(lang, 'token.copy')}
            </button>
          </div>

          <div className="label">{t(lang, 'preview.label')}</div>
          <a
            className={`preview-link${expired ? ' is-expired' : ''}`}
            href={session.preview_url}
            target="_blank"
            rel="noreferrer"
          >
            {session.preview_url.replace(/^https:\/\//, '')}
          </a>
        </section>
      )}

      <div className="footer">
        <a href="mailto:abuse@localhost">{t(lang, 'abuse.link')}</a>
      </div>
      <div className="sr-only" aria-live="polite">
        {liveMsg}
      </div>
    </main>
  )
}
