<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { canDeleteAccount } from '../api'
import { useAccountsStore } from '../stores/accounts'
import { applyTheme, readTheme, showH5FirstLoginBanner, toggleTheme } from '../theme'
import { loginStateLabel } from '../messages'

const store = useAccountsStore()
const { accounts, settings, loading, error, hasLoggedIn } = storeToRefs(store)
const theme = ref(readTheme())
const loginOpen = ref(false)
const backupRoot = reactive({})
const newPassword = reactive({})
const deleting = ref(null)
const deletePassword = ref('')
const deleteConfirmed = ref(false)
const busy = ref(false)

const showBanner = computed(() =>
  showH5FirstLoginBanner({
    base: import.meta.env.BASE_URL,
    hasLoggedIn: hasLoggedIn.value,
  }),
)

const deleteReady = computed(() => {
  if (!deleting.value) return false
  return canDeleteAccount({
    hasPassword: !!settings.value[deleting.value.id]?.has_password,
    password: deletePassword.value,
    confirmed: deleteConfirmed.value,
  })
})

onMounted(async () => {
  applyTheme(theme.value)
  await store.fetchAccounts()
  for (const a of store.accounts) {
    backupRoot[a.id] = store.settings[a.id]?.backup_root || ''
  }
})

function onToggleTheme() {
  theme.value = toggleTheme(theme.value)
}

function openLogin() {
  loginOpen.value = true
}

async function savePath(a) {
  busy.value = true
  try {
    await store.saveBackupRoot(a.id, backupRoot[a.id] || '')
  } catch {
    /* store.error */
  } finally {
    busy.value = false
  }
}

async function savePassword(a) {
  const pwd = newPassword[a.id] || ''
  if (!pwd) {
    store.error = '请输入访问密码'
    return
  }
  busy.value = true
  try {
    await store.setPassword(a.id, pwd)
    newPassword[a.id] = ''
  } catch {
    /* store.error */
  } finally {
    busy.value = false
  }
}

function openDelete(a) {
  deleting.value = a
  deletePassword.value = ''
  deleteConfirmed.value = false
}

function closeDelete() {
  deleting.value = null
  deletePassword.value = ''
  deleteConfirmed.value = false
}

async function confirmDelete() {
  if (!deleting.value || !deleteReady.value) return
  busy.value = true
  try {
    await store.removeAccount(deleting.value.id, {
      password: deletePassword.value,
      confirmed: deleteConfirmed.value,
    })
    closeDelete()
  } catch {
    /* store.error */
  } finally {
    busy.value = false
  }
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
          <button class="weui-btn warn" type="button" @click="openDelete(a)">删除</button>
          <router-link class="weui-btn plain" :to="{ name: 'chat', params: { accountId: a.id } }">
            聊天
          </router-link>
        </div>
        <div class="settings-title">备份设置</div>
        <label class="field">
          <span>备份路径</span>
          <input v-model="backupRoot[a.id]" type="text" autocomplete="off" />
        </label>
        <div class="actions">
          <button class="weui-btn" type="button" :disabled="busy" @click="savePath(a)">保存路径</button>
        </div>
        <label class="field">
          <span>访问密码</span>
          <input v-model="newPassword[a.id]" type="password" autocomplete="new-password" />
        </label>
        <div class="actions">
          <button class="weui-btn" type="button" :disabled="busy" @click="savePassword(a)">设置密码</button>
          <span v-if="settings[a.id]?.has_password" class="muted-inline">已设置</span>
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

    <div v-if="deleting" class="mask" @click.self="closeDelete">
      <div class="dialog dialog-form">
        <div>确认删除「{{ deleting.nickname || deleting.wxid || deleting.id }}」的备份？</div>
        <label class="field">
          <span>访问密码</span>
          <input v-model="deletePassword" type="password" autocomplete="current-password" />
        </label>
        <label class="check">
          <input v-model="deleteConfirmed" type="checkbox" />
          我确认删除此账号备份
        </label>
        <div class="actions">
          <button class="weui-btn plain" type="button" @click="closeDelete">取消</button>
          <button class="weui-btn warn" type="button" :disabled="busy || !deleteReady" @click="confirmDelete">
            确认删除
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
