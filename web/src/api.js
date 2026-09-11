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

export function getAccounts() {
  return fetch('/v1/accounts').then(parseJSON)
}

export function getConversations(accountId) {
  const q = new URLSearchParams({ account_id: accountId })
  return fetch('/v1/conversations?' + q.toString()).then(parseJSON)
}

export function getMessages(accountId, talkerId) {
  const q = new URLSearchParams({ account_id: accountId })
  const id = encodeURIComponent(talkerId)
  return fetch(`/v1/conversations/${id}/messages?` + q.toString()).then(parseJSON)
}
