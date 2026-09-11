<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useChatStore } from '../stores/chat'
import { formatTime, kindLabel, messagePreview } from '../messages'
import { applyTheme, readTheme, toggleTheme } from '../theme'

const props = defineProps({
  accountId: { type: String, required: true },
  talkerId: { type: String, default: '' },
})

const store = useChatStore()
const { conversations, messages, loadingConv, loadingMsg, error, current } = storeToRefs(store)
const theme = ref(readTheme())

const withTalker = computed(() => Boolean(props.talkerId))
const title = computed(() => current.value?.display_name || '聊天')

onMounted(() => {
  applyTheme(theme.value)
})

watch(
  () => [props.accountId, props.talkerId],
  async ([accountId, talkerId]) => {
    if (accountId !== store.accountId) {
      await store.loadConversations(accountId)
    }
    await store.loadMessages(talkerId)
  },
  { immediate: true },
)

function onToggleTheme() {
  theme.value = toggleTheme(theme.value)
}

function isSys(msg) {
  return msg.msg_type === 10000
}
</script>

<template>
  <div class="chat-layout" :class="{ 'with-talker': withTalker }">
    <header class="nav">
      <div class="nav-actions">
        <router-link v-if="withTalker" class="weui-btn link" :to="{ name: 'chat', params: { accountId } }">
          会话
        </router-link>
        <router-link v-else class="weui-btn link" :to="{ name: 'home' }">账号</router-link>
      </div>
      <h1>{{ withTalker ? title : '会话' }}</h1>
      <div class="nav-actions">
        <button class="icon-btn" type="button" @click="onToggleTheme">
          {{ theme === 'dark' ? '浅色' : '深色' }}
        </button>
      </div>
    </header>

    <aside class="conv-list">
      <p v-if="loadingConv" class="muted">加载中…</p>
      <p v-else-if="error && !conversations.length" class="error">{{ error }}</p>
      <p v-else-if="!conversations.length" class="empty">暂无会话</p>
      <router-link
        v-for="c in conversations"
        :key="c.talker_id"
        class="conv-item"
        :class="{ active: c.talker_id === talkerId }"
        :to="{ name: 'chat', params: { accountId, talkerId: c.talker_id } }"
      >
        <div class="conv-name">
          {{ c.display_name || c.talker_id }}
          <span class="tag muted">{{ kindLabel(c.kind) }}</span>
        </div>
        <div class="conv-sub">{{ c.msg_count }} 条 · {{ formatTime(c.last_msg_time) }}</div>
      </router-link>
    </aside>

    <section class="msg-pane">
      <div class="msg-head">{{ talkerId ? title : '选择会话' }}</div>
      <div class="msg-list">
        <p v-if="!talkerId" class="muted">从左侧选择会话</p>
        <p v-else-if="loadingMsg" class="muted">加载中…</p>
        <p v-else-if="error" class="error">{{ error }}</p>
        <p v-else-if="!messages.length" class="empty">暂无消息</p>
        <template v-else>
          <div
            v-for="m in messages"
            :key="m.msg_id"
            class="bubble-row"
            :class="{ send: m.is_send, sys: isSys(m) }"
          >
            <div class="bubble" :class="{ sys: isSys(m) }">
              {{ messagePreview(m) }}
              <div v-if="!isSys(m)" class="bubble-time">{{ formatTime(m.create_time) }}</div>
            </div>
          </div>
        </template>
      </div>
    </section>
  </div>
</template>
