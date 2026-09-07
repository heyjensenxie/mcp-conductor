<template>
  <div class="settings-page">
    <header class="page-head">
      <p class="eyebrow">SYSTEM · CONSOLE</p>
      <h2 class="page-title">{{ t('page.settings') }}</h2>
    </header>

    <!-- 基础设置 -->
    <a-card class="settings-card" :bordered="true">
      <template #title>
        <span class="card-title">{{ t('settings.basicTitle') }}</span>
      </template>
      <div class="form-grid">
        <div class="form-row">
          <span class="row-label">{{ t('settings.instanceName') }}</span>
          <a-input
            v-model:value="basic.instanceName"
            class="row-control"
            :maxlength="48"
            :placeholder="t('settings.instanceNamePh')"
          />
        </div>
        <div class="form-row">
          <span class="row-label">{{ t('settings.defaultLanguage') }}</span>
          <a-select v-model:value="basic.language" class="row-control" :options="languageOptions" />
        </div>
      </div>
      <div class="card-footer">
        <a-button type="primary" :disabled="!basicDirty" @click="saveBasic">
          {{ t('settings.saveChanges') }}
        </a-button>
      </div>
    </a-card>

    <!-- 观测与回放（运行期动态配置：入参捕获开关 + 一键清除已捕获入参） -->
    <a-card class="settings-card" :bordered="true">
      <template #title>
        <span class="card-title">{{ t('settings.observabilityTitle') }}</span>
        <span class="card-desc">{{ t('settings.observabilityDesc') }}</span>
      </template>

      <a-alert
        v-if="obs.persisted"
        type="info"
        show-icon
        class="obs-alert"
        :message="t('settings.sourceRuntime')"
      />
      <a-alert v-else type="warning" show-icon class="obs-alert" :message="t('settings.sourceStatic')" />

      <div class="form-grid">
        <div class="form-row">
          <span class="row-label">{{ t('settings.captureArgs') }}</span>
          <div class="row-control obs-control">
            <a-switch v-model:checked="obs.recordArgs" :loading="obs.loading" />
            <span class="obs-state">{{ obs.recordArgs ? t('settings.captureOn') : t('settings.captureOff') }}</span>
          </div>
        </div>
        <p class="row-hint">{{ t('settings.captureArgsDesc') }}</p>
      </div>

      <div class="card-footer obs-footer">
        <a-button danger :disabled="obs.purging" @click="purgeVisible = true">
          {{ t('settings.purgeArgs') }}
        </a-button>
        <a-button type="primary" :loading="obs.saving" @click="saveObservability">
          {{ t('settings.saveChanges') }}
        </a-button>
      </div>
    </a-card>

    <!-- 程序化访问 -->
    <a-card class="settings-card" :bordered="true">
      <template #title>
        <span class="card-title">{{ t('settings.accessTitle') }}</span>
        <span class="card-desc">{{ t('settings.accessDesc') }}</span>
      </template>

      <div v-if="!token" class="token-empty">
        <p class="token-empty-title">{{ t('settings.noCredential') }}</p>
        <a-button type="primary" @click="createToken">{{ t('settings.createCredential') }}</a-button>
      </div>

      <div v-else class="token-panel">
        <div class="token-head">
          <span class="token-name">{{ t('settings.automationToken') }}</span>
          <span class="token-status"><span class="mc-dot mc-dot--ok"></span>{{ t('settings.enabled') }}</span>
        </div>
        <div class="token-masked">
          <code class="mono">{{ maskedToken }}</code>
          <a-button type="text" size="small" @click="copyToken">
            <template #icon><CopyOutlined /></template>
          </a-button>
        </div>
        <div class="token-meta">
          <div class="meta-row">
            <span class="meta-label">{{ t('settings.createdTime') }}</span>
            <span class="meta-value">{{ createdAtText }}</span>
          </div>
          <div class="meta-row">
            <span class="meta-label">{{ t('settings.lastUsed') }}</span>
            <span class="meta-value">{{ t('settings.neverUsed') }}</span>
          </div>
        </div>
        <div class="token-actions">
          <a-button type="text" size="small" @click="regenerateVisible = true">{{ t('settings.regenerate') }}</a-button>
          <a-button type="text" size="small" danger @click="revokeVisible = true">{{ t('settings.revoke') }}</a-button>
        </div>
      </div>
    </a-card>

    <!-- 系统信息 -->
    <a-card class="settings-card" :bordered="true">
      <template #title>
        <span class="card-title">{{ t('settings.systemTitle') }}</span>
      </template>
      <div class="info-grid">
        <div class="info-row">
          <span class="row-label">{{ t('settings.version') }}</span>
          <span class="info-value mono">{{ appVersion }}</span>
        </div>
        <div class="info-row">
          <span class="row-label">{{ t('settings.mcpEndpoint') }}</span>
          <span class="info-value inline">
            <code class="mono">{{ mcpEndpoint }}</code>
            <a-button type="text" size="small" @click="copyEndpoint">
              <template #icon><CopyOutlined /></template>
            </a-button>
          </span>
        </div>
        <div class="info-row">
          <span class="row-label">{{ t('settings.build') }}</span>
          <span class="info-value"><code class="mono">{{ buildCommit }}</code></span>
        </div>
        <div class="info-row">
          <span class="row-label">{{ t('settings.buildTime') }}</span>
          <span class="info-value">{{ buildTime }}</span>
        </div>
        <div class="info-row">
          <span class="row-label">{{ t('settings.runtimeStatus') }}</span>
          <span class="info-value status-value">
            <span class="mc-dot" :class="health === 'ok' ? 'mc-dot--ok' : 'mc-dot--idle'"></span>
            {{ healthText }}
          </span>
        </div>
      </div>
    </a-card>

    <!-- 重新生成 / 撤销：二次确认 -->
    <a-modal
      v-model:open="regenerateVisible"
      :title="t('settings.regenTitle')"
      :ok-text="t('settings.regenOk')"
      :cancel-text="t('common.cancel')"
      @ok="doRegenerate"
    >
      <p class="confirm-desc">{{ t('settings.regenDesc') }}</p>
    </a-modal>

    <a-modal
      v-model:open="revokeVisible"
      :title="t('settings.revokeTitle')"
      :ok-text="t('settings.revokeOk')"
      :ok-button-props="{ danger: true }"
      :cancel-text="t('common.cancel')"
      @ok="doRevoke"
    >
      <p class="confirm-desc danger">{{ t('settings.revokeDesc') }}</p>
    </a-modal>

    <!-- 清除已捕获入参：二次确认（不可恢复） -->
    <a-modal
      v-model:open="purgeVisible"
      :title="t('settings.purgeTitle')"
      :ok-text="t('settings.purgeOk')"
      :ok-button-props="{ danger: true }"
      :cancel-text="t('common.cancel')"
      :confirm-loading="obs.purging"
      @ok="doPurgeArgs"
    >
      <p class="confirm-desc danger">{{ t('settings.purgeDesc') }}</p>
    </a-modal>

    <!-- 新建 / 重新生成后：一次性展示完整凭证 -->
    <a-modal v-model:open="secretVisible" :title="t('settings.automationToken')" width="520px" :footer="null">
      <a-alert type="warning" show-icon class="secret-alert" :message="t('settings.secretOnce')" />
      <div class="secret-row">
        <code class="mono secret-code">{{ pendingSecret }}</code>
        <a-button @click="copySecret">
          <template #icon><CopyOutlined /></template>{{ t('settings.copy') }}
        </a-button>
      </div>
      <div class="secret-foot">
        <a-button type="primary" @click="secretVisible = false">{{ t('settings.done') }}</a-button>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { CopyOutlined } from '@ant-design/icons-vue'
import { useI18n } from 'vue-i18n'
import { currentLocale, setLocale, type AppLocale } from '@/i18n'
import { parseBackendTime } from '@/utils/time'
import { DEFAULT_INSTANCE_NAME, useAppStore } from '@/stores/app'
import { getRuntimeConfig, putRuntimeConfig, purgeTrafficArgs } from '@/api'
import type { RuntimeConfig } from '@/types'

const { t } = useI18n()
const store = useAppStore()

/* ================= 基础设置（前端脚手架：本地持久化，接入后端后替换） ================= */

const basic = reactive({
  instanceName: store.instanceName,
  language: currentLocale(),
})
// 已保存快照：任一字段偏离即视为有未保存修改。
const saved = reactive({ ...basic })

const languageOptions = computed(() => [
  { value: 'zh-CN', label: t('lang.zh') },
  { value: 'en-US', label: t('lang.en') },
])

const basicDirty = computed(
  () => basic.instanceName !== saved.instanceName || basic.language !== saved.language,
)

function saveBasic() {
  // 实例名留空时回退默认值，保证始终有明确身份；经 store 写入，侧栏/登录页/标签标题即时生效。
  const name = basic.instanceName.trim() || DEFAULT_INSTANCE_NAME
  basic.instanceName = name
  store.setInstanceName(name)
  setLocale(basic.language as AppLocale)
  Object.assign(saved, basic)
  message.success(t('settings.savedOk'))
}

/* ================= 观测与回放（运行期动态配置：入参捕获开关 + 一键清除） =================
   与防护页（SecurityView）共用 GET/PUT /api/runtime-config。PUT 为**全量快照覆盖**，
   保存须带回当前 ratelimit/auto_ban/黑白名单 + 本次 record_args，避免误清其它运行期设置。 */

const obs = reactive({
  recordArgs: false, // 入参捕获开关（运行期可后台切换，无需重启）
  persisted: false, // 是否已有后台保存值（false=当前为 config.yaml 种子）
  loading: false,
  saving: false,
  purging: false,
})
const purgeVisible = ref(false)
// 最近一次 GET 到的生效运行期配置；保存时作为其余字段的基线全量回传。
let runtimeSnapshot: RuntimeConfig | null = null

async function loadObservability() {
  obs.loading = true
  try {
    const res = await getRuntimeConfig()
    runtimeSnapshot = res.config
    obs.recordArgs = res.config.observability?.record_args ?? false
    obs.persisted = res.persisted
  } catch (e) {
    runtimeSnapshot = null
    message.error(String(e))
  } finally {
    obs.loading = false
  }
}

async function saveObservability() {
  if (!runtimeSnapshot) {
    message.warning(t('settings.observabilityLoadFirst'))
    return
  }
  obs.saving = true
  try {
    await putRuntimeConfig({
      ratelimit: runtimeSnapshot.ratelimit,
      auto_ban: runtimeSnapshot.auto_ban,
      ip_blocklist: runtimeSnapshot.ip_blocklist ?? [],
      ip_whitelist: runtimeSnapshot.ip_whitelist ?? [],
      observability: { record_args: obs.recordArgs },
    })
    runtimeSnapshot = { ...runtimeSnapshot, observability: { record_args: obs.recordArgs } }
    obs.persisted = true
    message.success(t('settings.savedOk'))
  } catch (e) {
    message.error(String(e))
  } finally {
    obs.saving = false
  }
}

async function doPurgeArgs() {
  purgeVisible.value = false
  obs.purging = true
  try {
    const res = await purgeTrafficArgs()
    message.success(t('settings.purgeDone', { n: res.purged }))
  } catch (e) {
    message.error(String(e))
  } finally {
    obs.purging = false
  }
}

/* ================= 程序化访问（Automation Token） =================
   本地自管凭证脚手架：生成/重新生成/撤销均在前端完成，不触碰控制台自身登录
   凭据（store.token）。后端接入后仅需替换持久化层与生成函数。 */

const TOKEN_KEY = 'mc_automation_token'
const TOKEN_META_KEY = 'mc_automation_token_meta'

const token = ref('')
const createdAt = ref('')
const pendingSecret = ref('')
const secretVisible = ref(false)
const regenerateVisible = ref(false)
const revokeVisible = ref(false)

const maskedToken = computed(() => maskToken(token.value))
const createdAtText = computed(() => (createdAt.value ? formatDateTime(createdAt.value) : '—'))

function loadToken() {
  token.value = localStorage.getItem(TOKEN_KEY) ?? ''
  const raw = localStorage.getItem(TOKEN_META_KEY)
  if (raw) {
    try {
      createdAt.value = (JSON.parse(raw) as { created_at?: string }).created_at ?? ''
    } catch {
      createdAt.value = ''
    }
  }
}

function saveTokenRecord(newToken: string) {
  const now = new Date().toISOString()
  localStorage.setItem(TOKEN_KEY, newToken)
  localStorage.setItem(TOKEN_META_KEY, JSON.stringify({ created_at: now }))
  token.value = newToken
  createdAt.value = now
}

// 生成 mcp_ + 24 字节随机数（hex，48 位），仅弹窗展示一次。
function generateToken(): string {
  const bytes = new Uint8Array(24)
  crypto.getRandomValues(bytes)
  return 'mcp_' + Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

function createToken() {
  const next = generateToken()
  saveTokenRecord(next)
  pendingSecret.value = next
  secretVisible.value = true
  message.success(t('settings.tokenCreated'))
}

function doRegenerate() {
  regenerateVisible.value = false
  const next = generateToken()
  saveTokenRecord(next)
  pendingSecret.value = next
  secretVisible.value = true
  message.success(t('settings.tokenRegenerated'))
}

function doRevoke() {
  revokeVisible.value = false
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(TOKEN_META_KEY)
  token.value = ''
  createdAt.value = ''
  message.success(t('settings.tokenRevoked'))
}

// maskToken 脱敏：保留 mcp_+前 4 位与末尾 4 位，中间以圆点遮蔽。
function maskToken(value: string): string {
  if (!value) return ''
  if (value.length <= 12) return value
  return `${value.slice(0, 8)}••••••••••••${value.slice(-4)}`
}

/* ================= 系统信息 ================= */

const appVersion = 'v0.1.0'
const mcpEndpoint = '/mcp'
const buildCommit = __APP_COMMIT__
const buildTime = __APP_BUILD_TIME__
const health = ref<'checking' | 'ok' | 'down'>('checking')

const healthText = computed(() => {
  if (health.value === 'ok') return t('settings.running')
  if (health.value === 'down') return t('settings.offline')
  return '…'
})

/* ================= 通用：复制 / 时间格式 ================= */

async function copyText(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    message.success(t('settings.copied'))
  } catch {
    message.warning(t('settings.copyFailed'))
  }
}
function copyToken() {
  void copyText(token.value)
}
function copyEndpoint() {
  void copyText(mcpEndpoint)
}
function copySecret() {
  void copyText(pendingSecret.value)
}

function formatDateTime(iso: string): string {
  const d = parseBackendTime(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

onMounted(async () => {
  loadToken()
  void loadObservability() // 运行期动态配置（入参捕获开关）；独立失败不影响其余设置卡加载
  // 运行状态：命中免鉴权的 /healthz 存活探针，返回 data.status === 'ok'。
  try {
    const res = await fetch('/healthz')
    const body = (await res.json().catch(() => null)) as { data?: { status?: string } } | null
    health.value = res.ok && body?.data?.status === 'ok' ? 'ok' : 'down'
  } catch {
    health.value = 'down'
  }
})
</script>

<style scoped>
.settings-page {
  max-width: 1040px;
}

/* ---- 页头 ---- */
.page-head {
  margin: 2px 0 18px;
}
.eyebrow {
  margin: 0 0 4px;
  font-family: var(--mc-mono);
  font-size: 11px;
  letter-spacing: 0.14em;
  color: var(--mc-ink-3);
}
.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  line-height: 1.3;
  letter-spacing: 0.01em;
  color: var(--mc-ink);
}

/* ---- 卡片 ---- */
.settings-card {
  margin-bottom: 18px;
  border-color: var(--mc-line-soft);
  box-shadow: none;
}
.settings-card :deep(.ant-card-head) {
  min-height: 48px;
  padding: 0 20px;
  border-bottom: 1px solid var(--mc-line-soft);
}
.settings-card :deep(.ant-card-body) {
  padding: 2px 20px 12px;
}
.card-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--mc-ink);
}
.card-desc {
  margin-left: 10px;
  font-size: 12.5px;
  font-weight: 400;
  color: var(--mc-ink-3);
}

/* ---- 行布局（表单 / 系统信息共用惰性 label） ---- */
.row-label {
  flex: none;
  font-size: 13px;
  color: var(--mc-ink-2);
}

/* 基础设置 */
.form-grid {
  padding: 4px 0;
}
.form-row {
  display: grid;
  grid-template-columns: 168px minmax(0, 360px);
  align-items: center;
  gap: 16px;
  padding: 13px 0;
  border-bottom: 1px solid var(--mc-line-soft);
}
.form-row:last-child {
  border-bottom: none;
}
.row-control {
  width: 100%;
}
.card-footer {
  display: flex;
  justify-content: flex-end;
  padding: 16px 0 6px;
  border-top: 1px solid var(--mc-line-soft);
}

/* 观测与回放 */
.obs-alert {
  margin: 12px 0 6px;
}
.obs-control {
  display: flex;
  align-items: center;
  gap: 10px;
}
.obs-state {
  font-size: 12.5px;
  color: var(--mc-ink-2);
}
.row-hint {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.7;
  color: var(--mc-ink-3);
}
.obs-footer {
  gap: 8px;
}

/* 程序化访问 */
.token-empty {
  padding: 16px 0 18px;
}
.token-empty-title {
  margin: 0 0 16px;
  font-size: 13px;
  color: var(--mc-ink-2);
}
.token-panel {
  padding: 6px 0 8px;
}
.token-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
}
.token-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--mc-ink);
}
.token-status {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  color: var(--mc-ink-2);
}
.token-masked {
  display: flex;
  align-items: center;
  gap: 8px;
}
.token-masked code {
  font-family: var(--mc-mono);
  font-size: 13px;
  color: var(--mc-ink);
  background: #f6f8fb;
  border: 1px solid var(--mc-line-soft);
  border-radius: 6px;
  padding: 6px 10px;
}
.token-meta {
  margin: 16px 0 10px;
  padding-top: 12px;
  border-top: 1px solid var(--mc-line-soft);
}
.meta-row {
  display: flex;
  align-items: baseline;
  padding: 6px 0;
}
.meta-label {
  flex: none;
  width: 168px;
  font-size: 13px;
  color: var(--mc-ink-2);
}
.meta-value {
  font-size: 13px;
  color: var(--mc-ink);
}
.token-actions {
  display: flex;
  gap: 4px;
  padding-top: 4px;
}

/* 系统信息 */
.info-grid {
  padding: 4px 0;
}
.info-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 11px 0;
  border-bottom: 1px solid var(--mc-line-soft);
}
.info-row:last-child {
  border-bottom: none;
}
.info-row .row-label {
  width: 168px;
}
.info-value {
  font-size: 13px;
  color: var(--mc-ink);
}
.info-value code {
  font-family: var(--mc-mono);
  font-size: 12.5px;
  color: var(--mc-ink);
  background: #f6f8fb;
  border: 1px solid var(--mc-line-soft);
  border-radius: 5px;
  padding: 3px 7px;
}
.inline {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.status-value {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

/* 确认弹窗 */
.confirm-desc {
  margin: 6px 0 0;
  font-size: 13px;
  line-height: 1.7;
  color: var(--mc-ink-2);
}
.confirm-desc.danger {
  color: var(--mc-danger);
}

/* 一次性凭证弹窗 */
.secret-alert {
  margin-bottom: 16px;
}
.secret-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.secret-code {
  flex: 1;
  min-width: 0;
  font-family: var(--mc-mono);
  font-size: 13px;
  color: var(--mc-ink);
  background: #f6f8fb;
  border: 1px solid var(--mc-line-soft);
  border-radius: 6px;
  padding: 8px 10px;
  word-break: break-all;
}
.secret-foot {
  display: flex;
  justify-content: flex-end;
  margin-top: 18px;
}
</style>