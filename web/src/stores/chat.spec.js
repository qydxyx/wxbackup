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
    const p1 = store.loadMessages('t1')
    const p2 = store.loadMessages('t2')
    first.resolve(jsonRes({ messages: [{ msg_id: 'old', text: 'stale' }] }))
    second.resolve(jsonRes({ messages: [{ msg_id: 'new', text: 'fresh' }] }))
    await Promise.all([p1, p2])
    expect(store.talkerId).toBe('t2')
    expect(store.messages.map((m) => m.msg_id)).toEqual(['new'])
  })
})
