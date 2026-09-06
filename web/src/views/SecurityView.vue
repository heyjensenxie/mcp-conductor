<template>
  <a-spin :spinning="loading">
    <section class="sec-hero">
      <div>
        <p class="eyebrow">DATA PLANE · GOVERNANCE</p>
        <h2>{{ t('page.security') }}</h2>
        <p>{{ t('security.rateHint') }}</p>
      </div>
    </section>

    <a-alert
      v-if="viewLoaded && !ratelimitEnabled"
      type="warning"
      show-icon
      class="mb"
      :message="t('security.inert')"
    />
    <a-alert
      v-if="viewLoaded"
      :type="persisted ? 'success' : 'default'"
      show-icon
      class="mb"
      :message="persisted ? t('security.sourceRuntime') : t('security.sourceStatic')"
    />

    <!-- 数据面三级限流阈值 -->
    <a-card class="mb" :title="t('security.rateTitle')">
      <a-row :gutter="[16, 8]" class="window-row">
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.windowSec') }}</div>
          <a-input-number v-model:value="form.ratelimit.window_seconds" :min="1" :max="3600" :style="{ width: '100%' }" placeholder="1" />
        </a-col>
        <a-col :xs="24" :md="16">
          <div class="rate-label">&nbsp;</div>
          <div class="card-hint window-hint">{{ t('security.windowHint') }}</div>
        </a-col>
      </a-row>
      <a-row :gutter="[16, 8]">
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.defaultQps') }}</div>
          <a-input-number v-model:value="form.ratelimit.qps" :min="0" :style="{ width: '100%' }" placeholder="0" />
        </a-col>
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.defaultBurst') }}</div>
          <a-input-number v-model:value="form.ratelimit.burst" :min="0" :style="{ width: '100%' }" placeholder="0" />
        </a-col>
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.ipQps') }}</div>
          <a-input-number v-model:value="form.ratelimit.ip_qps" :min="0" :style="{ width: '100%' }" placeholder="0" />
        </a-col>
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.ipBurst') }}</div>
          <a-input-number v-model:value="form.ratelimit.ip_burst" :min="0" :style="{ width: '100%' }" placeholder="0" />
        </a-col>
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.globalQps') }}</div>
          <a-input-number v-model:value="form.ratelimit.global_qps" :min="0" :style="{ width: '100%' }" placeholder="0" />
        </a-col>
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.globalBurst') }}</div>
          <a-input-number v-model:value="form.ratelimit.global_burst" :min="0" :style="{ width: '100%' }" placeholder="0" />
        </a-col>
      </a-row>
      <div class="rate-foot">
        <a-tag v-if="Number(form.ratelimit.global_qps) <= 0" color="default">{{ t('security.globalOff') }}</a-tag>
        <span class="card-hint">{{ t('security.rateHint') }}</span>
      </div>
    </a-card>

    <!-- 自动封禁 -->
    <a-card class="mb" :title="t('security.autoBanTitle')">
      <div class="ab-switch-row">
        <a-switch v-model:checked="autoBan.enabled" />
        <span class="card-hint">{{ t('security.autoBanHint') }}</span>
      </div>
      <a-row :gutter="[16, 8]" :class="{ 'ab-disabled': !autoBan.enabled }">
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.autoBanWindow') }}</div>
          <a-input-number v-model:value="autoBan.window_seconds" :min="1" :max="3600" :style="{ width: '100%' }" />
        </a-col>
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.autoBanMax') }}</div>
          <a-input-number v-model:value="autoBan.max_violations" :min="1" :max="10000" :style="{ width: '100%' }" />
        </a-col>
        <a-col :xs="24" :md="8">
          <div class="rate-label">{{ t('security.autoBanTtl') }}</div>
          <a-input-number v-model:value="autoBan.ban_seconds" :min="1" :max="86400" :style="{ width: '100%' }" />
        </a-col>
      </a-row>
    </a-card>

    <!-- IP 封禁名单 -->
    <a-card :title="t('security.blocklist')">
      <p class="card-hint">{{ t('security.blocklistHint') }}</p>
      <div class="add-row">
        <a-input
          v-model:value="addInput"
          :placeholder="t('security.addIpPlaceholder')"
          style="width: 320px"
          allow-clear
          @press-enter="addBlock"
        />
        <a-button type="primary" @click="addBlock">{{ t('security.add') }}</a-button>
      </div>
      <div class="bl-tags">
        <a-empty v-if="form.blocklist.length === 0" :description="t('security.empty')" :image-style="{ height: '40px' }" />
        <a-tag
          v-for="(item, idx) in form.blocklist"
          :key="item"
          closable
          class="bl-tag mono"
          @close="removeBlock(idx)"
        >
          {{ item }}
        </a-tag>
      </div>
    </a-card>

    <!-- IP 白名单（可信豁免） -->
    <a-card :title="t('security.whitelist')">
      <p class="card-hint">{{ t('security.whitelistHint') }}</p>
      <div class="add-row">
        <a-input
          v-model:value="whitelistInput"
          :placeholder="t('security.addIpPlaceholder')"
          style="width: 320px"
          allow-clear
          @press-enter="addWhitelist"
        />
        <a-button type="primary" @click="addWhitelist">{{ t('security.add') }}</a-button>
      </div>
      <div class="bl-tags">
        <a-empty v-if="whitelist.length === 0" :description="t('security.empty')" :image-style="{ height: '40px' }" />
        <a-tag
          v-for="(item, idx) in whitelist"
          :key="item"
          closable
          color="blue"
          class="bl-tag mono"
          @close="removeWhitelist(idx)"
        >
          {{ item }}
        </a-tag>
      </div>
    </a-card>

    <div class="save-bar">
      <a-button type="primary" :loading="saving" @click="save">
        {{ saving ? t('security.saving') : t('security.save') }}
      </a-button>
    </div>
  </a-spin>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { getRuntimeConfig, putRuntimeConfig } from '@/api'
import type { RuntimeRateLimit } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const viewLoaded = ref(false)
const persisted = ref(false)
const ratelimitEnabled = ref(true)
const addInput = ref('')
const whitelist = ref<string[]>([])
const whitelistInput = ref('')

// 空值以 0 处理（0 = 沿用默认 / 关闭某级）；antd InputNumber 清空时为 null。
type Field = number | null
const form = reactive<{ ratelimit: Record<keyof RuntimeRateLimit, Field>; blocklist: string[] }>({
  ratelimit: { qps: 0, burst: 0, window_seconds: 60, ip_qps: 0, ip_burst: 0, global_qps: 0, global_burst: 0 },
  blocklist: [],
})

// 自动封禁参数（来源在检测窗口内被限流达次数即临时封禁，TTL 自动解封）。
const autoBan = reactive<{ enabled: boolean; window_seconds: Field; max_violations: Field; ban_seconds: Field }>({
  enabled: false,
  window_seconds: 60,
  max_violations: 5,
  ban_seconds: 300,
})

onMounted(async () => {
  loading.value = true
  try {
    await loadConfig()
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
    viewLoaded.value = true
  }
})

async function loadConfig() {
  const res = await getRuntimeConfig()
  persisted.value = res.persisted
  ratelimitEnabled.value = res.ratelimit_enabled
  const rl = res.config.ratelimit
  ;(Object.keys(form.ratelimit) as (keyof RuntimeRateLimit)[]).forEach((k) => {
    form.ratelimit[k] = rl[k]
  })
  form.blocklist = [...(res.config.ip_blocklist ?? [])]
  whitelist.value = [...(res.config.ip_whitelist ?? [])]
  const ab = res.config.auto_ban
  if (ab) {
    autoBan.enabled = !!ab.enabled
    autoBan.window_seconds = ab.window_seconds
    autoBan.max_violations = ab.max_violations
    autoBan.ban_seconds = ab.ban_seconds
  }
}

function addBlock() {
  const ip = addInput.value.trim()
  if (!ip) return
  if (!form.blocklist.includes(ip)) form.blocklist.push(ip)
  addInput.value = ''
}

function removeBlock(idx: number) {
  form.blocklist.splice(idx, 1)
}

function addWhitelist() {
  const ip = whitelistInput.value.trim()
  if (!ip) return
  if (!whitelist.value.includes(ip)) whitelist.value.push(ip)
  whitelistInput.value = ''
}

function removeWhitelist(idx: number) {
  whitelist.value.splice(idx, 1)
}

async function save() {
  const n = (v: Field) => Number(v ?? 0)
  saving.value = true
  try {
    const payload = {
      ratelimit: {
        qps: n(form.ratelimit.qps),
        burst: n(form.ratelimit.burst),
        window_seconds: n(form.ratelimit.window_seconds),
        ip_qps: n(form.ratelimit.ip_qps),
        ip_burst: n(form.ratelimit.ip_burst),
        global_qps: n(form.ratelimit.global_qps),
        global_burst: n(form.ratelimit.global_burst),
      },
      auto_ban: {
        enabled: autoBan.enabled,
        window_seconds: n(autoBan.window_seconds),
        max_violations: n(autoBan.max_violations),
        ban_seconds: n(autoBan.ban_seconds),
      },
      ip_blocklist: form.blocklist,
      ip_whitelist: whitelist.value,
    }
    await putRuntimeConfig(payload)
    persisted.value = true
    message.success(t('security.saved'))
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.sec-hero {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  margin: 2px 0 22px;
  padding: 22px 28px;
  border: 1px solid #d9e5f5;
  border-radius: 12px;
  background: linear-gradient(112deg, #f7fbff, #eef5ff 58%, #f9fcff);
}
.sec-hero h2 { margin: 2px 0 5px; font-size: 24px; color: #102a43; }
.sec-hero p { margin: 0; color: #627d98; max-width: 680px; }
.eyebrow { color: #1677ff !important; font-family: var(--mc-mono); font-size: 11px; letter-spacing: 0.12em; }
.mb { margin-bottom: 16px; }
.window-row { margin-bottom: 10px; }
.window-hint { color: #7a8aa0; font-size: 12.5px; margin-top: 6px; line-height: 1.6; }
.rate-label { margin-bottom: 6px; color: #627d98; font-size: 12px; }
.ab-switch-row { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; }
.ab-disabled { opacity: 0.5; pointer-events: none; }
.rate-foot { display: flex; align-items: center; gap: 8px; margin-top: 12px; }
.card-hint { margin: 0 0 6px; color: #7a8aa0; font-size: 12.5px; line-height: 1.6; }
.add-row { margin: 10px 0 6px; display: flex; gap: 8px; }
.bl-tags { margin-top: 12px; display: flex; flex-wrap: wrap; gap: 8px; }
.bl-tag { padding: 3px 10px; font-size: 13px; }
.save-bar { margin-top: 18px; display: flex; justify-content: flex-end; }
</style>
