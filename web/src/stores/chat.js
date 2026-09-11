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
    convGen: 0,
    msgGen: 0,
  }),
  getters: {
    current: (s) => s.conversations.find((c) => c.talker_id === s.talkerId) || null,
  },
  actions: {
    async loadConversations(accountId) {
      const gen = ++this.convGen
      this.accountId = accountId
      this.loadingConv = true
      this.error = ''
      try {
        const data = await getConversations(accountId)
        if (gen !== this.convGen) return
        this.conversations = Array.isArray(data?.conversations) ? data.conversations : []
      } catch (e) {
        if (gen !== this.convGen) return
        this.error = e.message || String(e)
        this.conversations = []
      } finally {
        if (gen === this.convGen) this.loadingConv = false
      }
    },
    async loadMessages(talkerId) {
      const gen = ++this.msgGen
      const talker = talkerId || ''
      this.talkerId = talker
      const accountId = this.accountId
      if (!accountId || !talker) {
        if (gen === this.msgGen) {
          this.messages = []
          this.loadingMsg = false
        }
        return
      }
      this.loadingMsg = true
      this.error = ''
      try {
        const data = await getMessages(accountId, talker)
        if (gen !== this.msgGen) return
        const list = Array.isArray(data?.messages) ? data.messages : []
        this.messages = list.slice().reverse()
      } catch (e) {
        if (gen !== this.msgGen) return
        this.error = e.message || String(e)
        this.messages = []
      } finally {
        if (gen === this.msgGen) this.loadingMsg = false
      }
    },
  },
})
