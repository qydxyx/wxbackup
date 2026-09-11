import { defineStore } from 'pinia'
import {
  canDeleteAccount,
  deleteAccount,
  getAccountSettings,
  getAccounts,
  putAccountPassword,
  putAccountSettings,
} from '../api'

export const useAccountsStore = defineStore('accounts', {
  state: () => ({
    accounts: [],
    settings: {},
    loading: false,
    error: '',
  }),
  getters: {
    hasLoggedIn: (s) => s.accounts.some((a) => a.login_state === 'logged_in'),
  },
  actions: {
    async fetchAccounts() {
      this.loading = true
      this.error = ''
      try {
        const data = await getAccounts()
        this.accounts = Array.isArray(data?.accounts) ? data.accounts : []
        await Promise.all(
          this.accounts.map((a) => this.fetchSettings(a.id).catch(() => {})),
        )
      } catch (e) {
        this.error = e.message || String(e)
        this.accounts = []
      } finally {
        this.loading = false
      }
    },
    async fetchSettings(id) {
      const data = await getAccountSettings(id)
      this.settings = { ...this.settings, [id]: data }
      return data
    },
    async saveBackupRoot(id, backupRoot) {
      this.error = ''
      try {
        const data = await putAccountSettings(id, backupRoot)
        this.settings = { ...this.settings, [id]: data }
        return data
      } catch (e) {
        this.error = e.message || String(e)
        throw e
      }
    },
    async setPassword(id, password) {
      this.error = ''
      try {
        await putAccountPassword(id, password)
        const cur = this.settings[id] || {}
        this.settings = { ...this.settings, [id]: { ...cur, has_password: true } }
      } catch (e) {
        this.error = e.message || String(e)
        throw e
      }
    },
    async removeAccount(id, { password, confirmed } = {}) {
      const hasPassword = !!this.settings[id]?.has_password
      if (!canDeleteAccount({ hasPassword, password, confirmed })) {
        const err = new Error(hasPassword && !password ? '请输入访问密码' : '请确认删除')
        this.error = err.message
        throw err
      }
      this.error = ''
      try {
        await deleteAccount(id, password)
        this.accounts = this.accounts.filter((a) => a.id !== id)
        const next = { ...this.settings }
        delete next[id]
        this.settings = next
      } catch (e) {
        this.error = e.message || String(e)
        throw e
      }
    },
  },
})
