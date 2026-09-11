import { defineStore } from 'pinia'
import { getConversations, getMessages } from '../api'

export const useChatStore = defineStore('chat', {
  state: () => ({
    accountId: '',
    conversations: [],
    talkerId: '',
    messages: [],
    loadingConv: false,
    loadingMsg: false,
    error: '',
  }),
  getters: {
    current: (s) => s.conversations.find((c) => c.talker_id === s.talkerId) || null,
  },
  actions: {
    async loadConversations(accountId) {
      this.accountId = accountId
      this.loadingConv = true
      this.error = ''
      try {
        const data = await getConversations(accountId)
        this.conversations = Array.isArray(data?.conversations) ? data.conversations : []
      } catch (e) {
        this.error = e.message || String(e)
        this.conversations = []
      } finally {
        this.loadingConv = false
      }
    },
    async loadMessages(talkerId) {
      this.talkerId = talkerId || ''
      if (!this.accountId || !this.talkerId) {
        this.messages = []
        return
      }
      this.loadingMsg = true
      this.error = ''
      try {
        const data = await getMessages(this.accountId, this.talkerId)
        const list = Array.isArray(data?.messages) ? data.messages : []
        this.messages = list.slice().reverse()
      } catch (e) {
        this.error = e.message || String(e)
        this.messages = []
      } finally {
        this.loadingMsg = false
      }
    },
  },
})
