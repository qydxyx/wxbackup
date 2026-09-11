import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAccountsStore } from './accounts'
import { messagePreview } from '../messages'
import { applyTheme, isCgiBase, showH5FirstLoginBanner } from '../theme'

describe('accounts store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('loads accounts from GET /v1/accounts', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        statusText: 'OK',
        text: async () =>
          JSON.stringify({
            accounts: [{ id: 'a1', nickname: 'Fixture User', login_state: 'logged_in' }],
          }),
      }),
    )
    const store = useAccountsStore()
    await store.fetchAccounts()
    expect(fetch).toHaveBeenCalledWith('/v1/accounts')
    expect(store.accounts).toHaveLength(1)
    expect(store.accounts[0].id).toBe('a1')
    expect(store.hasLoggedIn).toBe(true)
    expect(store.error).toBe('')
  })

  it('records API errors', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        statusText: 'Bad Gateway',
        text: async () => JSON.stringify({ error: { message: 'internal error' } }),
      }),
    )
    const store = useAccountsStore()
    await store.fetchAccounts()
    expect(store.accounts).toEqual([])
    expect(store.error).toBe('internal error')
    expect(store.hasLoggedIn).toBe(false)
  })
})

describe('messagePreview', () => {
  it('uses text for type 1 and placeholders for others', () => {
    expect(messagePreview({ msg_type: 1, text: 'hello fixture' })).toBe('hello fixture')
    expect(messagePreview({ msg_type: 3 })).toBe('[图片]')
    expect(messagePreview({ msg_type: 34 })).toBe('[语音]')
    expect(messagePreview({ msg_type: 43 })).toBe('[视频]')
    expect(messagePreview({ msg_type: 10000, text: 'synthetic system notice' })).toBe(
      'synthetic system notice',
    )
  })
})

describe('theme helpers', () => {
  it('toggles data-weui-theme and dark class', () => {
    applyTheme('dark')
    expect(document.documentElement.getAttribute('data-weui-theme')).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    applyTheme('light')
    expect(document.documentElement.getAttribute('data-weui-theme')).toBe('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('shows H5 first-login banner only off cgi without a logged-in account', () => {
    expect(isCgiBase('/cgi/ThirdParty/WxBackup/index.cgi/')).toBe(true)
    expect(isCgiBase('/')).toBe(false)
    expect(showH5FirstLoginBanner({ base: '/', hasLoggedIn: false })).toBe(true)
    expect(showH5FirstLoginBanner({ base: '/', hasLoggedIn: true })).toBe(false)
    expect(
      showH5FirstLoginBanner({
        base: '/cgi/ThirdParty/WxBackup/index.cgi/',
        hasLoggedIn: false,
      }),
    ).toBe(false)
  })
})
