<template>
  <div class="login-page">
    <a-card class="login-card" :bordered="false">
      <div class="brand">
        <img src="/logo.png" alt="MCP Conductor" class="logo" />
        <div class="brand-name mono">MCP Conductor</div>
        <div class="brand-sub mono">The control plane for your MCP ecosystem.</div>
      </div>

      <a-alert v-if="!authRequired" type="info" show-icon class="alert">
        <template #message>{{ t('auth.disabledHint') }}</template>
        <template #action>
          <a-button size="small" type="primary" @click="router.replace('/')">{{ t('auth.enter') }}</a-button>
        </template>
      </a-alert>

      <a-form v-else :label-col="{ span: 5 }" :wrapper-col="{ span: 19 }" class="form">
        <a-form-item :label="t('auth.username')">
          <a-input v-model:value="username" :placeholder="t('auth.usernamePh')" @pressEnter="doLogin" />
        </a-form-item>
        <a-form-item :label="t('auth.password')" :required="true">
          <a-input-password v-model:value="password" :placeholder="t('auth.passwordPh')" @pressEnter="doLogin" />
        </a-form-item>
        <a-form-item :wrapper-col="{ offset: 5, span: 19 }">
          <a-button type="primary" block :loading="loggingIn" @click="doLogin">{{ t('auth.submit') }}</a-button>
        </a-form-item>
        <div class="hint">{{ t('auth.passwordHint') }}</div>
      </a-form>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { login } from '@/api'
import { ensureAuthRequired, invalidateAuthState } from '@/auth/session'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useAppStore()

const username = ref('')
const password = ref('')
const loggingIn = ref(false)
const authRequired = ref(true)

onMounted(async () => {
  // 登录页不带旧凭据，避免残留头干扰。
  store.clearToken()
  invalidateAuthState()
  authRequired.value = await ensureAuthRequired()
})

async function doLogin() {
  if (!password.value.trim()) {
    message.warning(t('settings.loginPwdRequired'))
    return
  }
  loggingIn.value = true
  try {
    const session = await login({ username: username.value.trim() || 'admin', password: password.value })
    store.setToken(session.token)
    message.success(t('auth.loginOk'))
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    await router.replace(redirect)
  } catch (e) {
    message.error(String(e))
  } finally {
    loggingIn.value = false
  }
}
</script>

<style scoped>
.login-page {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--mc-sidebar);
}
.login-card {
  width: 420px;
  background: var(--mc-elev);
  border-radius: 10px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.25);
}
.brand {
  text-align: center;
  margin-bottom: 20px;
}
.logo {
  width: 56px;
  height: 56px;
  object-fit: contain;
}
.brand-name {
  font-size: 22px;
  font-weight: 700;
  color: var(--mc-ink);
  margin-top: 8px;
}
.brand-sub {
  font-size: 12px;
  color: var(--mc-ink-2);
  margin-top: 4px;
  letter-spacing: 0.04em;
}
.alert {
  margin-bottom: 12px;
}
.hint {
  color: #999;
  font-size: 12px;
  line-height: 1.6;
  padding-left: 5px;
}
</style>
