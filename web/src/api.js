// cgi iframe is reverse-proxied under BASE_URL; origin-root /v1 never reaches index.cgi
export function apiURL(path, base = import.meta.env.BASE_URL) {
  const root = String(base ?? '/').replace(/\/+$/, '')
  const p = path.startsWith('/') ? path : '/' + path
  return root + p
}

async function parseJSON(res) {
  const text = await res.text()
  let data = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }
  if (!res.ok) {
    const msg = data?.error?.message || res.statusText || 'request failed'
    throw new Error(msg)
  }
  return data
}

function sendJSON(method, path, body) {
  return fetch(apiURL(path), {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body ?? {}),
  }).then(parseJSON)
}

export function getAccounts() {
  return fetch(apiURL('/v1/accounts')).then(parseJSON)
}

export function getAccountSettings(accountId) {
  const id = encodeURIComponent(accountId)
  return fetch(apiURL(`/v1/accounts/${id}/settings`)).then(parseJSON)
}

export function putAccountSettings(accountId, backupRoot) {
  const id = encodeURIComponent(accountId)
  return sendJSON('PUT', `/v1/accounts/${id}/settings`, { backup_root: backupRoot })
}

export function putAccountPassword(accountId, password) {
  const id = encodeURIComponent(accountId)
  return sendJSON('PUT', `/v1/accounts/${id}/password`, { password })
}

export function verifyAccountPassword(accountId, password) {
  const id = encodeURIComponent(accountId)
  return sendJSON('POST', `/v1/accounts/${id}/password/verify`, { password })
}

export function deleteAccount(accountId, password) {
  const id = encodeURIComponent(accountId)
  return sendJSON('DELETE', `/v1/accounts/${id}`, { password: password || '' })
}

export function canDeleteAccount({ hasPassword, password, confirmed }) {
  if (!confirmed) return false
  if (hasPassword && !String(password || '')) return false
  return true
}

export function getConversations(accountId) {
  const q = new URLSearchParams({ account_id: accountId })
  return fetch(apiURL('/v1/conversations') + '?' + q.toString()).then(parseJSON)
}

export function getMessages(accountId, talkerId) {
  const q = new URLSearchParams({ account_id: accountId })
  const id = encodeURIComponent(talkerId)
  return fetch(apiURL(`/v1/conversations/${id}/messages`) + '?' + q.toString()).then(parseJSON)
}
