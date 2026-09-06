<template>
  <a-card :bordered="true">
    <template #title>{{ t('page.observability') }}</template>
    <template #extra>
      <a-space :size="8" wrap>
        <a-radio-group v-model:value="scope" size="small" button-style="solid" @change="onScopeChange">
          <a-radio-button value="tool">{{ t('observability.scopeTool') }}</a-radio-button>
          <a-radio-button value="server">{{ t('observability.scopeServer') }}</a-radio-button>
          <a-radio-button value="instance">{{ t('observability.scopeInstance') }}</a-radio-button>
        </a-radio-group>
        <a-select
          v-if="scope === 'instance'"
          v-model:value="serverId"
          size="small"
          style="width: 180px"
          :placeholder="t('observability.selectServer')"
          :options="serverOptions"
          @change="load"
        />
        <a-select
          v-model:value="windowMinutes"
          size="small"
          style="width: 120px"
          :options="windowOptions"
          @change="load"
        />
        <a-select
          v-model:value="dimKey"
          size="small"
          style="width: 160px"
          allow-clear
          :placeholder="t('observability.dimAll')"
          :options="dimOptions"
          @change="load"
        />
        <a-button size="small" @click="load">
          <template #icon><ReloadOutlined /></template>{{ t('observability.refresh') }}
        </a-button>
      </a-space>
    </template>

    <div v-if="hasTrend" class="trend-block">
      <div class="trend-head mono">{{ trendTitle }}</div>
      <TrafficTrend :categories="trendSeries.categories" :totals="trendSeries.totals" :rates="trendSeries.rates" />
    </div>

    <a-table :data-source="rows" :columns="columns" :loading="loading" :pagination="false" :row-key="(r: any) => r.key">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'success_rate'">{{ (record.success_rate * 100).toFixed(1) }}%</template>
      </template>
    </a-table>
  </a-card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { getMetrics, getServerMetrics, getInstanceMetrics, getMetricsTrend, listAllServers } from '@/api'
import type { MCPServer, MetricSnapshot, TrendPoint } from '@/types'
import TrafficTrend from '@/components/TrafficTrend.vue'

const { t } = useI18n()
const scope = ref<'tool' | 'server' | 'instance'>('tool')
const windowMinutes = ref(30)
const dimKey = ref<string | undefined>(undefined)
const servers = ref<MCPServer[]>([])
const serverId = ref<string | undefined>(undefined)
const metrics = ref<MetricSnapshot[]>([])
const trendData = ref<TrendPoint[]>([])
const loading = ref(false)

const windowOptions = computed(() => [
  { value: 30, label: t('observability.window30m') },
  { value: 60, label: t('observability.window1h') },
  { value: 360, label: t('observability.window6h') },
  { value: 1440, label: t('observability.window1d') },
  { value: 4320, label: t('observability.window3d') },
  { value: 10080, label: t('observability.window7d') },
])
const serverOptions = computed(() => servers.value.map((s) => ({ value: s.id, label: `${s.name}（${s.id}）` })))

function fmtMin(ts: number): string {
  const d = new Date(ts * 1000)
  // 长窗（跨日）带日期，短窗只显时间。
  const hhmm = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  if (windowMinutes.value >= 24 * 60) {
    return `${d.getMonth() + 1}-${d.getDate()} ${hhmm}`
  }
  return hhmm
}

const trendSeries = computed(() => ({
  categories: trendData.value.map((p) => fmtMin(p.ts)),
  totals: trendData.value.map((p) => p.totals),
  rates: trendData.value.map((p) => (p.totals ? Math.round(((p.totals - p.errors) / p.totals) * 100) : 0)),
}))
const hasTrend = computed(() => trendData.value.some((p) => p.totals > 0))

// 展示名称：tool=gateway 名；server/instance 去掉聚合前缀；dim 聚焦时标注。
const scopeLabel = computed(() => {
  switch (scope.value) {
    case 'server':
      return t('observability.scopeServer')
    case 'instance':
      return t('observability.scopeInstance')
    default:
      return t('observability.scopeTool')
  }
})
const trendTitle = computed(() => {
  const windowLabel = windowOptions.value.find((o) => o.value === windowMinutes.value)?.label ?? ''
  const base = `${scopeLabel.value}${windowLabel ? ' · ' + windowLabel : ''}`
  return dimKey.value ? `${base} · ${dimKey.value}` : base
})

// 快照表行：按 scope 展示裸维度名（工具名 / server id / instance id）。
function displayKey(raw: string): string {
  if (scope.value === 'server' && raw.startsWith('server:')) return raw.slice('server:'.length)
  if (scope.value === 'instance' && raw.startsWith('instance:')) {
    const rest = raw.slice('instance:'.length)
    const i = rest.indexOf(':')
    return i > 0 ? rest.slice(i + 1) : rest
  }
  return raw
}
const rows = computed<MetricSnapshot[]>(() => metrics.value.map((m) => ({ ...m, key: displayKey(m.key) })))

// 维度聚焦选项（来自快照行；选中后趋势只查该维度）。
const dimOptions = computed(() => {
  const seen = new Map<string, string>()
  for (const m of metrics.value) {
    const val = displayKey(m.key)
    if (val && !seen.has(val)) seen.set(val, val)
  }
  return Array.from(seen.values()).map((v) => ({ value: v, label: v }))
})

const columns = computed<any[]>(() => [
  { title: t('observability.dimension'), key: 'key', dataIndex: 'key' },
  { title: t('observability.requests'), key: 'totals', dataIndex: 'totals', width: 110 },
  { title: t('observability.success'), key: 'success', dataIndex: 'success', width: 100 },
  { title: t('observability.errors'), key: 'errors', dataIndex: 'errors', width: 100 },
  { title: t('observability.successRate'), key: 'success_rate', width: 120 },
  { title: t('observability.p50'), key: 'p50', dataIndex: 'p50', width: 110 },
  { title: t('observability.p95'), key: 'p95', dataIndex: 'p95', width: 110 },
  { title: t('observability.p99'), key: 'p99', dataIndex: 'p99', width: 110 },
])

async function onScopeChange() {
  dimKey.value = undefined
  // 进入 instance 维自动选首个 Server（尚未选时）。
  if (scope.value === 'instance' && !serverId.value && servers.value.length) {
    serverId.value = servers.value[0].id
  }
  await load()
}

function trendParams() {
  const extra: Record<string, string> = {}
  if (scope.value === 'instance' && serverId.value) extra.server_id = serverId.value
  if (dimKey.value) extra.dim_key = dimKey.value
  return extra
}

async function load() {
  // instance 维需要已选定 Server；目录尚未就绪时保持空态，避免对后端发起缺参请求。
  if (scope.value === 'instance' && !serverId.value) {
    metrics.value = []
    trendData.value = []
    return
  }
  loading.value = true
  try {
    let snapshots: MetricSnapshot[]
    if (scope.value === 'server') snapshots = await getServerMetrics()
    else if (scope.value === 'instance') snapshots = await getInstanceMetrics(serverId.value ?? '')
    else snapshots = await getMetrics()
    const trendRes = await getMetricsTrend(scope.value, windowMinutes.value, trendParams())
    metrics.value = snapshots
    trendData.value = trendRes.series
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  try {
    servers.value = await listAllServers()
  } catch {
    // Server 目录不可用不影响默认 tool 维加载。
  }
  await load()
})
</script>

<style scoped>
.trend-block {
  margin-bottom: 16px;
}
.trend-head {
  font-size: 12px;
  color: var(--mc-ink-2);
  letter-spacing: 0.06em;
  margin-bottom: 4px;
}
</style>
