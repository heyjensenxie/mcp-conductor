<template>
  <div class="login-page">
    <a-config-provider :theme="loginTheme">
      <main class="login-card">
        <div class="brand">
          <img src="/logo.png" alt="MCP Conductor" class="logo" />
          <h1 class="brand-name">{{ store.instanceName }}</h1>
          <p class="brand-sub">The control plane for your MCP ecosystem.</p>
        </div>

        <a-alert v-if="!authRequired" type="info" show-icon class="notice">
          <template #message>{{ t('auth.disabledHint') }}</template>
          <template #action>
            <a-button size="small" type="primary" @click="router.replace('/')">
              {{ t('auth.enter') }}
            </a-button>
          </template>
        </a-alert>

        <form v-else class="form" novalidate @submit.prevent="doLogin">
          <div class="field" :class="{ 'is-error': fieldError.username }">
            <label class="field-label" for="login-username">{{ t('auth.username') }}</label>
            <a-input
              id="login-username"
              v-model:value="username"
              size="large"
              autocomplete="username"
              :placeholder="t('auth.usernamePh')"
              @input="onFieldInput('username')"
            />
          </div>

          <div class="field" :class="{ 'is-error': fieldError.password }">
            <label class="field-label" for="login-password">{{ t('auth.password') }}</label>
            <a-input-password
              id="login-password"
              v-model:value="password"
              size="large"
              autocomplete="current-password"
              :placeholder="t('auth.passwordPh')"
              @input="onFieldInput('password')"
            />
            <transition name="fade-slide">
              <p v-if="pwError" class="field-error">{{ pwError }}</p>
            </transition>
          </div>

          <transition name="fade-slide">
            <div v-if="authError" class="auth-error" role="alert">
              <ExclamationCircleFilled class="auth-error-icon" />
              <span>{{ authError }}</span>
            </div>
          </transition>

          <a-button
            type="primary"
            html-type="submit"
            block
            size="large"
            :loading="loggingIn"
            class="login-submit"
          >
            {{ t('auth.submit') }}
          </a-button>
        </form>
      </main>
    </a-config-provider>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { ExclamationCircleFilled } from '@ant-design/icons-vue'
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

// 登录页局部主题：贴合品牌蓝紫（Logo 靛色家族），只作用于本卡片内的组件。
const loginTheme = {
  token: {
    colorPrimary: '#4f46e5',
    colorInfo: '#4f46e5',
    colorError: '#e0453c',
    colorText: '#1e2530',
    colorTextSecondary: '#5b6472',
    colorBorder: '#dfe3ea',
    colorBorderSecondary: '#e9edf3',
    colorBgContainer: '#ffffff',
    borderRadius: 8,
    borderRadiusLG: 8,
    fontSize: 14,
    fontSizeLG: 14,
    controlHeight: 40,
    controlHeightLG: 40,
    controlOutline: 'rgba(79, 70, 229, 0.14)',
    controlOutlineWidth: 3,
  },
}

// 字段级错误：pwError 为必填提示；authError 为服务端校验失败（账号/密码错误、限流等）。
const pwError = ref('')
const authError = ref('')
const fieldError = reactive({ username: '', password: '' })

onMounted(async () => {
  // 登录页不带旧凭据，避免残留头干扰。
  store.clearToken()
  invalidateAuthState()
  authRequired.value = await ensureAuthRequired()
  // 实例名持久化后同步标签标题。
  document.title = store.instanceName
})

function onFieldInput(field: 'username' | 'password') {
  // 用户重新输入时立即清除该字段错误与服务端错误。
  fieldError[field] = ''
  pwError.value = ''
  authError.value = ''
}

// cleanAuthError 去掉 axios 的 "Error: " 前缀与后端信封的 "<error_code>: " 前缀，
// 只保留可读信息（如「用户名或密码错误」）。
function cleanAuthError(msg: string): string {
  return msg.replace(/^Error:\s*/i, '').replace(/^[a-zA-Z][a-z0-9_]*:\s*/, '')
}

async function doLogin() {
  if (loggingIn.value) return
  pwError.value = ''
  authError.value = ''
  fieldError.username = ''
  fieldError.password = ''

  if (!password.value.trim()) {
    fieldError.password = 'error'
    pwError.value = t('auth.passwordRequired')
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
    fieldError.username = 'error'
    fieldError.password = 'error'
    authError.value = cleanAuthError(String(e))
  } finally {
    loggingIn.value = false
  }
}
</script>

<style scoped>
.login-page {
  position: relative;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 48px 20px 56px;
  background:
    radial-gradient(1100px 720px at 50% -12%, rgba(99, 102, 241, 0.12), transparent 62%),
    radial-gradient(900px 640px at 92% 112%, rgba(31, 111, 235, 0.08), transparent 60%),
    linear-gradient(180deg, #0b1120 0%, #0a101d 100%);
  overflow: hidden;
}

/* 极淡精细网格，增添工程空间层次 */
.login-page::before {
  content: '';
  position: absolute;
  inset: 0;
  background-image: linear-gradient(rgba(148, 163, 200, 0.05) 1px, transparent 1px),
    linear-gradient(90deg, rgba(148, 163, 200, 0.05) 1px, transparent 1px);
  background-size: 26px 26px;
  pointer-events: none;
}

/* 极淡 MCP 节点 / 连接关系纹理，仅作空间层次 */
.login-page::after {
  content: '';
  position: absolute;
  inset: 0;
  background-image: url("data:image/svg+xml,%3Csvg%20xmlns%3D%27http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%27%20width%3D%27240%27%20height%3D%27240%27%20viewBox%3D%270%200%20240%20240%27%3E%3Cg%20fill%3D%27none%27%20stroke%3D%27%238fa0cc%27%20stroke-width%3D%271%27%3E%3Cpath%20d%3D%27M44%2058%20L96%20104%20M96%20104%20L168%2074%20M96%20104%20L104%20140%20M104%20140%20L192%20158%27%20opacity%3D%270.55%27%2F%3E%3Ccircle%20cx%3D%2744%27%20cy%3D%2758%27%20r%3D%273%27%20fill%3D%27%238fa0cc%27%20opacity%3D%270.5%27%2F%3E%3Ccircle%20cx%3D%2796%27%20cy%3D%27104%27%20r%3D%274%27%20fill%3D%27%238fa0cc%27%20opacity%3D%270.7%27%2F%3E%3Ccircle%20cx%3D%27168%27%20cy%3D%2774%27%20r%3D%272.5%27%20fill%3D%27%238fa0cc%27%20opacity%3D%270.45%27%2F%3E%3Ccircle%20cx%3D%27104%27%20cy%3D%27140%27%20r%3D%273%27%20fill%3D%27%238fa0cc%27%20opacity%3D%270.6%27%2F%3E%3Ccircle%20cx%3D%27192%27%20cy%3D%27158%27%20r%3D%272.5%27%20fill%3D%27%238fa0cc%27%20opacity%3D%270.4%27%2F%3E%3C%2Fg%3E%3C%2Fsvg%3E");
  background-size: 240px 240px;
  opacity: 0.12;
  pointer-events: none;
}

.login-card {
  position: relative;
  z-index: 1;
  width: 420px;
  max-width: 100%;
  padding: 46px 40px;
  background: linear-gradient(180deg, #ffffff 0%, #f8fafd 100%);
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 14px;
  box-shadow:
    0 0 0 1px rgba(13, 21, 32, 0.06),
    0 2px 5px rgba(4, 8, 18, 0.22),
    0 26px 64px -22px rgba(4, 8, 18, 0.52);
  animation: card-in 0.36s cubic-bezier(0.2, 0.7, 0.3, 1) both;
}
@keyframes card-in {
  from {
    opacity: 0;
    transform: translateY(14px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.brand {
  text-align: center;
  padding-top: 2px;
}
.logo {
  width: 76px;
  height: auto;
  object-fit: contain;
  display: inline-block;
}
.brand-name {
  margin: 22px 0 0;
  font-family: var(--mc-mono);
  font-size: 24px;
  font-weight: 700;
  line-height: 1.25;
  letter-spacing: 0.01em;
  color: #171e2b;
}
.brand-sub {
  margin: 9px auto 0;
  max-width: 32ch;
  font-family: var(--mc-body);
  font-size: 13px;
  font-weight: 400;
  line-height: 1.5;
  color: #7a858f;
}

.form {
  margin-top: 38px;
}
.field {
  margin-bottom: 20px;
}
.field-label {
  display: block;
  margin-bottom: 7px;
  font-family: var(--mc-body);
  font-size: 13px;
  font-weight: 500;
  line-height: 20px;
  color: #3b4351;
}

/* 输入框：默认边框弱化，Focus 品牌蓝紫 + 极淡外发光 */
.field :deep(.ant-input),
.field :deep(.ant-input-affix-wrapper) {
  border-color: #dfe3ea;
  transition: border-color 0.18s ease, box-shadow 0.18s ease;
}
.field :deep(.ant-input)::placeholder,
.field :deep(.ant-input-affix-wrapper input::placeholder) {
  color: #a0a8b5;
}
.field :deep(.ant-input):hover,
.field :deep(.ant-input-affix-wrapper:hover) {
  border-color: #b3b4f2;
}
.field :deep(.ant-input):focus,
.field :deep(.ant-input-focused),
.field :deep(.ant-input-affix-wrapper-focused) {
  border-color: #5b54ea;
  box-shadow: 0 0 0 3px rgba(79, 70, 229, 0.14);
}
.field :deep(.ant-input-password-icon) {
  color: #8a94a3;
}
.field :deep(.ant-input-password-icon:hover) {
  color: #4f46e5;
}

/* 字段错误态 */
.field.is-error :deep(.ant-input),
.field.is-error :deep(.ant-input-affix-wrapper) {
  border-color: #e0453c;
}
.field.is-error :deep(.ant-input:focus),
.field.is-error :deep(.ant-input-affix-wrapper-focused) {
  box-shadow: 0 0 0 3px rgba(224, 69, 60, 0.12);
}
.field-error {
  margin: 7px 2px 0;
  font-family: var(--mc-body);
  font-size: 12px;
  line-height: 1.4;
  color: #d9443b;
}

/* 账号/密码错误（服务端） */
.auth-error {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 0 0 14px;
  padding: 9px 12px;
  border-radius: 8px;
  background: rgba(224, 69, 60, 0.06);
  border: 1px solid rgba(224, 69, 60, 0.18);
  font-family: var(--mc-body);
  font-size: 13px;
  line-height: 1.4;
  color: #b93a32;
}
.auth-error-icon {
  flex: none;
  font-size: 14px;
  color: #e0453c;
}

/* 主按钮：品牌蓝紫，Hover 微亮、Active 轻微下压 */
.login-submit {
  height: 42px;
  margin-top: 4px;
  border-radius: 8px;
  font-family: var(--mc-body);
  font-size: 15px;
  font-weight: 600;
  letter-spacing: 0.04em;
  box-shadow: 0 1px 2px rgba(79, 70, 229, 0.35), 0 12px 24px -12px rgba(79, 70, 229, 0.55);
  transition: box-shadow 0.18s ease, transform 0.12s ease;
}
.login-submit:not(:disabled):hover {
  box-shadow: 0 2px 6px rgba(79, 70, 229, 0.42), 0 16px 32px -14px rgba(79, 70, 229, 0.62);
}
.login-submit:active {
  transform: translateY(1px);
}

.notice {
  margin-top: 34px;
}

/* 错误提示淡入淡出 */
.fade-slide-enter-active,
.fade-slide-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}
.fade-slide-enter-from,
.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(-3px);
}
</style>