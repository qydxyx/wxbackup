import { BY_CODE, BY_KIND, Kind, MsgType } from './types'

function toInt(v) {
  if (v == null || v === '') return null
  const n = Number(v)
  return Number.isFinite(n) ? n : null
}

export function resolveInfo(msg) {
  if (!msg) return BY_CODE[MsgType.Unknown]
  const e = msg.extra || {}
  if (typeof e.kind === 'string' && BY_KIND[e.kind]) return BY_KIND[e.kind]
  const appType = toInt(e.app_type)
  if (appType != null && BY_CODE[appType] && appType !== MsgType.App) {
    return BY_CODE[appType]
  }
  const t = toInt(msg.msg_type)
  if (t != null && BY_CODE[t]) return BY_CODE[t]
  return BY_CODE[MsgType.Unknown]
}

export function resolveKind(msg) {
  return resolveInfo(msg).kind
}

export function isSystem(msg) {
  return resolveKind(msg) === Kind.system
}

export function isUnsupported(msg) {
  return !resolveInfo(msg).supported
}

export function messagePreview(msg) {
  if (!msg) return ''
  const info = resolveInfo(msg)
  if (info.kind === Kind.text || info.kind === Kind.system) {
    const text = (msg.text || '').trim()
    if (text) return text
    return info.kind === Kind.system ? info.preview : ''
  }
  if (info.preview) return info.preview
  const text = (msg.text || '').trim()
  return text || '[消息]'
}
