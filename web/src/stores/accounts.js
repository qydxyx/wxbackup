import { defineStore } from 'pinia'
import { getAccounts } from '../api'

export const useAccountsStore = defineStore('accounts', {
  state: () => ({
    accounts: [],
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
      } catch (e) {
        this.error = e.message || String(e)
        this.accounts = []
      } finally {
        this.loading = false
      }
    },
  },
})
