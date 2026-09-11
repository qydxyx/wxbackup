<script setup>
import { computed, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useAccountsStore } from '../stores/accounts'
import { applyTheme, readTheme, showH5FirstLoginBanner, toggleTheme } from '../theme'
import { loginStateLabel } from '../messages'

const store = useAccountsStore()
const { accounts, loading, error, hasLoggedIn } = storeToRefs(store)
const theme = ref(readTheme())
const loginOpen = ref(false)

const showBanner = computed(() =>
  showH5FirstLoginBanner({
    base: import.meta.env.BASE_URL,
    hasLoggedIn: hasLoggedIn.value,
  }),
)

onMounted(() => {
  applyTheme(theme.value)
  store.fetchAccounts()
})

function onToggleTheme() {
  theme.value = toggleTheme(theme.value)
}

function openLogin() {
  loginOpen.value = true
}
</script>

<template>
  <div class="page">
    <header class="nav">
      <h1>微信备份</h1>
      <div class="nav-actions">
        <button class="icon-btn" type="button" @click="onToggleTheme">
          {{ theme === 'dark' ? '浅色' : '深色' }}
        </button>
      </div>
    </header>

    <div v-if="showBanner" class="notice">请到 NAS 桌面完成首次登录</div>
    <div v-if="error" class="error">{{ error }}</div>

    <main class="content">
      <p v-if="loading" class="muted">加载中…</p>
      <div v-else-if="!accounts.length" class="card">
        <div class="empty">暂无账号</div>
        <div class="actions actions-center">
          <button class="weui-btn" type="button" @click="openLogin">登录</button>
        </div>
      </div>

      <article v-for="a in accounts" :key="a.id" class="card">
        <div class="card-title">
          {{ a.nickname || a.wxid || a.id }}
          <span class="tag" :class="{ muted: a.login_state !== 'logged_in' }">
            {{ loginStateLabel(a.login_state) }}
          </span>
        </div>
        <div class="card-meta">{{ a.wxid }}</div>
        <div class="actions">
          <button class="weui-btn" type="button" @click="openLogin">登录</button>
          <button class="weui-btn" type="button" disabled>备份</button>
          <button class="weui-btn" type="button" disabled>恢复</button>
          <button class="weui-btn warn" type="button" disabled>删除</button>
          <router-link class="weui-btn plain" :to="{ name: 'chat', params: { accountId: a.id } }">
            聊天
          </router-link>
        </div>
      </article>
    </main>

    <div v-if="loginOpen" class="mask" @click.self="loginOpen = false">
      <div class="dialog">
        <div>扫码登录即将提供</div>
        <div class="qr-placeholder">QR</div>
        <button class="weui-btn" type="button" @click="loginOpen = false">关闭</button>
      </div>
    </div>
  </div>
</template>
