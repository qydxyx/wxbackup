export const THEME_KEY = 'wxbackup-theme'

export function readTheme() {
  try {
    const v = localStorage.getItem(THEME_KEY)
    if (v === 'dark' || v === 'light') return v
  } catch {
    // private mode
  }
  return 'light'
}

export function applyTheme(theme) {
  const t = theme === 'dark' ? 'dark' : 'light'
  const root = document.documentElement
  root.setAttribute('data-weui-theme', t)
  root.classList.toggle('dark', t === 'dark')
  try {
    localStorage.setItem(THEME_KEY, t)
  } catch {
    // private mode
  }
  return t
}

export function toggleTheme(current) {
  return applyTheme(current === 'dark' ? 'light' : 'dark')
}

export function isCgiBase(base = import.meta.env.BASE_URL) {
  return String(base).includes('/cgi/ThirdParty/WxBackup/')
}

export function showH5FirstLoginBanner({ base, hasLoggedIn }) {
  return !isCgiBase(base) && !hasLoggedIn
}
