import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { apiURL, canDeleteAccount } from '../api'
import { useAccountsStore } from './accounts'
import { messagePreview } from '../messages'
import { applyTheme, isCgiBase, showH5FirstLoginBanner } from '../theme'

function jsonRes(body, ok = true) {
  return {
    ok,
    statusText: ok ? 'OK' : 'Error',
    text: async () => JSON.stringify(body),
  }
}

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
    expect(fetch).toHaveBeenCalledWith(apiURL('/v1/accounts'))
    expect(store.accounts).toHaveLength(1)
    expect(store.accounts[0].id).toBe('a1')
    expect(store.hasLoggedIn).toBe(true)
    expect(store.error).toBe('')
  })

  it('saves backup path and password then rejects delete without password', async () => {
    const fetchMock = vi.fn(async (url, opts = {}) => {
      const method = opts.method || 'GET'
      if (url === apiURL('/v1/accounts') && method === 'GET') {
        return jsonRes({ accounts: [{ id: 'a1', nickname: 'Fixture User', login_state: 'logged_in' }] })
      }
      if (url === apiURL('/v1/accounts/a1/settings') && method === 'GET') {
        return jsonRes({ backup_root: '/data/old', has_password: false })
      }
      if (url === apiURL('/v1/accounts/a1/settings') && method === 'PUT') {
        return jsonRes({ backup_root: JSON.parse(opts.body).backup_root, has_password: false })
      }
      if (url === apiURL('/v1/accounts/a1/password') && method === 'PUT') {
        return jsonRes({ ok: true, has_password: true })
      }
      if (url === apiURL('/v1/accounts/a1') && method === 'DELETE') {
        return jsonRes({ error: { code: 'password_required', message: 'access password is required' } }, false)
      }
      return jsonRes({ error: { message: 'unexpected ' + method + ' ' + url } }, false)
    })
    vi.stubGlobal('fetch', fetchMock)
    const store = useAccountsStore()
    await store.fetchAccounts()
    expect(store.settings.a1.backup_root).toBe('/data/old')

    await store.saveBackupRoot('a1', '/data/custom')
    expect(store.settings.a1.backup_root).toBe('/data/custom')

    await store.setPassword('a1', 'secret')
    expect(store.settings.a1.has_password).toBe(true)

    await expect(store.removeAccount('a1', { password: '', confirmed: true })).rejects.toThrow('请输入访问密码')
    expect(store.accounts).toHaveLength(1)
    expect(fetchMock).not.toHaveBeenCalledWith(apiURL('/v1/accounts/a1'), expect.objectContaining({ method: 'DELETE' }))
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

describe('canDeleteAccount', () => {
  it('rejects delete without confirm or password when hash is set', () => {
    expect(canDeleteAccount({ hasPassword: true, password: 'x', confirmed: false })).toBe(false)
    expect(canDeleteAccount({ hasPassword: true, password: '', confirmed: true })).toBe(false)
    expect(canDeleteAccount({ hasPassword: true, password: 'secret', confirmed: true })).toBe(true)
    expect(canDeleteAccount({ hasPassword: false, password: '', confirmed: true })).toBe(true)
  })
})

describe('apiURL', () => {
  it('keeps origin-root paths when base is /', () => {
    expect(apiURL('/v1/accounts', '/')).toBe('/v1/accounts')
    expect(apiURL('/v1/accounts')).toBe('/v1/accounts')
  })

  it('prefixes cgi BASE_URL so iframe traffic hits index.cgi', () => {
    expect(apiURL('/v1/accounts', '/cgi/ThirdParty/WxBackup/index.cgi/')).toBe(
      '/cgi/ThirdParty/WxBackup/index.cgi/v1/accounts',
    )
    expect(
      apiURL('/v1/conversations/wxid.friend/messages', '/cgi/ThirdParty/WxBackup/index.cgi/'),
    ).toBe('/cgi/ThirdParty/WxBackup/index.cgi/v1/conversations/wxid.friend/messages')
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
