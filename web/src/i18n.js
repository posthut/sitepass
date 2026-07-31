import en from '../../locales/en.json'
import ru from '../../locales/ru.json'
import kk from '../../locales/kk.json'

const catalogs = { en, ru, kk }

export function detectLanguage() {
  const stored = localStorage.getItem('sitepass.lang')
  if (stored && catalogs[stored]) return stored
  const nav = (navigator.languages || [navigator.language || 'en']).map((l) =>
    String(l).toLowerCase().slice(0, 2),
  )
  for (const code of nav) {
    if (catalogs[code]) return code
  }
  return 'en'
}

export function t(lang, key, vars = {}) {
  const catalog = catalogs[lang] || catalogs.en
  let text = catalog[key] || catalogs.en[key] || key
  for (const [name, value] of Object.entries(vars)) {
    text = text.replaceAll(`{${name}}`, String(value))
  }
  return text
}

export function setLanguage(lang) {
  if (!catalogs[lang]) return
  localStorage.setItem('sitepass.lang', lang)
  document.documentElement.lang = lang
}
