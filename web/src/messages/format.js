export function formatTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function loginStateLabel(state) {
  switch (state) {
    case 'logged_in':
      return '已登录'
    case 'pending':
      return '登录中'
    default:
      return '未登录'
  }
}

export function kindLabel(kind) {
  switch (kind) {
    case 'group':
      return '群'
    case 'oa':
      return '公众号'
    case 'wecom':
      return '企微'
    default:
      return '好友'
  }
}
