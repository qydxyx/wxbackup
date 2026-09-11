import { apiURL } from '../api'

export function extra(msg, key, fallback) {
  const e = msg && msg.extra
  if (!e || e[key] == null || e[key] === '') return fallback
  return e[key]
}

export function extraText(msg, key, fallback = '') {
  const v = extra(msg, key, undefined)
  if (v == null) return fallback
  return String(v)
}

export function extraNum(msg, key) {
  const v = extra(msg, key, undefined)
  if (v == null || v === '') return null
  const n = Number(v)
  return Number.isFinite(n) ? n : null
}

export function mediaURL(msg) {
  const id = extraText(msg, 'media_id')
  if (!id) return extraText(msg, 'url') || extraText(msg, 'thumb')
  const q = new URLSearchParams()
  if (msg.account_id) q.set('account_id', msg.account_id)
  const suffix = q.toString() ? `?${q.toString()}` : ''
  return apiURL(`/v1/media/${encodeURIComponent(id)}`) + suffix
}

export function formatSize(bytes) {
  const n = Number(bytes)
  if (!Number.isFinite(n) || n < 0) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

export function formatDuration(sec) {
  const n = Number(sec)
  if (!Number.isFinite(n) || n < 0) return ''
  const s = Math.round(n)
  const m = Math.floor(s / 60)
  const r = s % 60
  return `${m}:${String(r).padStart(2, '0')}`
}
