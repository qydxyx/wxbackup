import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useChatStore } from './chat'

function deferred() {
  let resolve
  const promise = new Promise((r) => {
    resolve = r
  })
  return { promise, resolve }
}

function jsonRes(body) {
  return {
    ok: true,
    statusText: 'OK',
    text: async () => JSON.stringify(body),
  }
}

async function flushUntil(pred, n = 30) {
  for (let i = 0; i < n; i++) {
    if (pred()) return
    await Promise.resolve()
  }
}

async function flush(n = 10) {
  for (let i = 0; i < n; i++) await Promise.resolve()
}

describe('chat store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('ignores a slower previous talker response', async () => {
    const first = deferred()
    const second = deferred()
    let n = 0
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => {
        n += 1
        return n === 1 ? first.promise : second.promise
      }),
    )
    const store = useChatStore()
    store.accountId = 'a1'
    const p1 = store.loadMessages('a1', 't1')
    const p2 = store.loadMessages('a1', 't2')
    first.resolve(jsonRes({ messages: [{ msg_id: 'old', text: 'stale' }] }))
    second.resolve(jsonRes({ messages: [{ msg_id: 'new', text: 'fresh' }] }))
    await Promise.all([p1, p2])
    expect(store.talkerId).toBe('t2')
    expect(store.messages.map((m) => m.msg_id)).toEqual(['new'])
  })

  it('does not let a stale route resume loadMessages after account switch', async () => {
    const pending = []
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url) => {
        const d = deferred()
        pending.push({ url: String(url), resolve: d.resolve, promise: d.promise })
        return d.promise
      }),
    )
    const store = useChatStore()
    const pA = store.syncRoute('a1', 't1')
    const pB = store.syncRoute('a2', 't2')
    expect(pending).toHaveLength(2)

    pending[1].resolve(jsonRes({ conversations: [{ talker_id: 't2' }] }))
    await flushUntil(() => pending.some((p) => p.url.includes('/messages')))
    pending[0].resolve(jsonRes({ conversations: [{ talker_id: 't1' }] }))
    await flush()

    const msgCalls = pending.filter((p) => p.url.includes('/messages'))
    expect(msgCalls).toHaveLength(1)
    expect(msgCalls[0].url).toContain('/conversations/t2/messages')
    expect(msgCalls[0].url).toContain('account_id=a2')

    msgCalls[0].resolve(jsonRes({ messages: [{ msg_id: 'b1', text: 'from B' }] }))
    await Promise.all([pA, pB])
    expect(store.accountId).toBe('a2')
    expect(store.talkerId).toBe('t2')
    expect(store.messages.map((m) => m.msg_id)).toEqual(['b1'])
  })
})
