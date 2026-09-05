<template>
  <div>
    <a-card :bordered="true" :title="t('settings.apiCredential')" class="mb">
      <a-row :gutter="16">
        <a-col :span="10">
          <a-input v-model:value="token" type="password" :placeholder="t('settings.apiKeyPlaceholder')" />
        </a-col>
        <a-col>
          <a-button type="primary" @click="save">{{ t('settings.save') }}</a-button>
        </a-col>
      </a-row>
    </a-card>

    <a-card v-if="authRequired" :bordered="true" :title="t('settings.loginTitle')" class="mb">
      <div v-if="!sessionActive" class="login-row">
        <a-input v-model:value="username" :placeholder="t('settings.username')" style="width: 180px" />
        <a-input-password v-model:value="password" :placeholder="t('settings.password')" style="width: 240px" @pressEnter="doLogin" />
        <a-button type="primary" :loading="loggingIn" @click="doLogin">{{ t('settings.loginBtn') }}</a-button>
        <span class="hint">{{ t('settings.loginHint') }}</span>
      </div>
      <a-space v-else>
        <span>{{ t('settings.sessionActive') }}</span>
        <a-button danger size="small" @click="doLogout">{{ t('settings.logoutBtn') }}</a-button>
      </a-space>
    </a-card>

    <a-card :bordered="true" :title="t('settings.about')" class="mb">
      <a-descriptions :column="1" bordered size="small">
        <a-descriptions-item :label="t('settings.version')">0.1.0</a-descriptions-item>
        <a-descriptions-item :label="t('settings.mcpEndpoint')">
          <code>/mcp</code>（streamable HTTP · tools/list · tools/call）
        </a-descriptions-item>
        <a-descriptions-item :label="t('settings.controlApi')">
          <code>/api/*</code>（code / message / request_id）
        </a-descriptions-item>
        <a-descriptions-item :label="t('settings.configDesc')">
          config.yaml + CONDUCTOR_*（server / database / redis / gateway / ratelimit / auth / credentials / logging / observability）
        </a-descriptions-item>
      </a-descriptions>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { getAuthStatus, login } from '@/api'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const store = useAppStore()
const token = ref(store.token)

const authRequired = ref(false)
const loggingIn = ref(false)
const username = ref('')
const password = ref('')
// 会话令牌以 mc1. 前缀标识；sessionActive 时提供登出。
const sessionActive = computed(() => store.token.startsWith('mc1.'))

onMounted(async () => {
  try {
    const st = await getAuthStatus()
    authRequired.value = st.auth_required
  } catch {
    authRequired.value = false
  }
})

function save() {
  store.setToken(token.value.trim())
  message.success(t('settings.savedOk'))
}

async function doLogin() {
  if (!password.value.trim()) {
    message.warning(t('settings.loginPwdRequired'))
    return
  }
  loggingIn.value = true
  try {
    const session = await login({ username: username.value.trim(), password: password.value })
    store.setToken(session.token)
    token.value = session.token
    password.value = ''
    message.success(t('settings.loginOk'))
  } catch (e) {
    message.error(String(e))
  } finally {
    loggingIn.value = false
  }
}

function doLogout() {
  store.setToken('')
  token.value = ''
  username.value = ''
  message.success(t('settings.logoutOk'))
}
</script>

<style scoped>
.mb {
  margin-bottom: 16px;
}
code {
  background: #f5f5f5;
  padding: 2px 6px;
  border-radius: 4px;
}
.login-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.hint {
  color: #999;
  font-size: 12px;
}
</style>