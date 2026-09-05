<template>
  <a-card :bordered="true">
    <template #title>{{ t('page.traffic') }}</template>
    <template #extra>
      <a-button size="small" @click="load">
        <template #icon><ReloadOutlined /></template>{{ t('traffic.refresh') }}
      </a-button>
    </template>

    <div v-if="trendHasData" class="trend-block">
      <div class="trend-head mono">{{ t('traffic.trendTitle') }}</div>
      <TrafficTrend :categories="trendSeries.categories" :totals="trendSeries.totals" :rates="trendSeries.rates" />
    </div>

    <div class="filters">
      <a-space wrap :size="8">
        <a-select v-model:value="serverId" :placeholder="t('traffic.filterServer')" style="width: 260px" allow-clear @change="load">
          <a-select-option v-for="s in servers" :key="s.id" :value="s.id">{{ s.name }}</a-select-option>
        </a-select>
        <a-select v-model:value="statusFilter" style="width: 140px" @change="() => {}">
          <a-select-option value="all">{{ t('traffic.statusAll') }}</a-select-option>
          <a-select-option value="success">{{ t('traffic.statusSuccess') }}</a-select-option>
          <a-select-option value="error">{{ t('traffic.statusError') }}</a-select-option>
        </a-select>
        <span class="hint">{{ t('traffic.scopeHint') }}</span>
        <a-tag>{{ t('traffic.count') }}：{{ filtered.length }}</a-tag>
      </a-space>
    </div>

    <a-table :data-source="filtered" :columns="columns" size="small" :pagination="false" :row-key="(r: any) => r.request_id">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'status'">
          <a-tag :color="record.status === 'success' ? 'green' : 'red'">{{ record.status }}</a-tag>
        </template>
        <template v-else-if="column.key === 'error'">
          <a-tooltip v-if="record.error" :title="record.error">
            <span class="err-cell">{{ record.error }}</span>
          </a-tooltip>
          <span v-else>-</span>
        </template>
      </template>
    </a-table>
  </a-card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { getLogs, getMetricsTrend, listServers } from '@/api'
import type { MCPServer, TrafficSample, TrendPoint } from '@/types'
import TrafficTrend from '@/components/TrafficTrend.vue'

const { t } = useI18n()
const logs = ref<TrafficSample[]>([])
const servers = ref<MCPServer[]>([])
const serverId = ref('')
const statusFilter = ref<'all' | 'success' | 'error'>('all')
const trendData = ref<TrendPoint[]>([])

function fmtMin(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

const trendSeries = computed(() => ({
  categories: trendData.value.map((p) => fmtMin(p.ts)),
  totals: trendData.value.map((p) => p.totals),
  rates: trendData.value.map((p) => (p.totals ? Math.round(((p.totals - p.errors) / p.totals) * 100) : 0)),
}))
const trendHasData = computed(() => trendData.value.some((p) => p.totals > 0))

// 状态过滤在前端执行（数据为最近 500 条）。
const filtered = computed(() => {
  if (statusFilter.value === 'all') return logs.value
  const ok = statusFilter.value === 'success'
  return logs.value.filter((l) => (l.status === 'success') === ok)
})

const columns = computed<any[]>(() => [
  { title: t('traffic.time'), key: 'timestamp', dataIndex: 'timestamp', width: 190 },
  { title: t('traffic.tool'), key: 'tool', dataIndex: 'tool' },
  { title: t('traffic.server'), key: 'server_id', dataIndex: 'server_id', width: 120 },
  { title: t('traffic.client'), key: 'client', dataIndex: 'client', width: 110 },
  { title: t('traffic.status'), key: 'status', dataIndex: 'status', width: 100 },
  { title: t('traffic.latencyMs'), key: 'latency_ms', dataIndex: 'latency_ms', width: 100 },
  { title: t('traffic.error'), key: 'error', dataIndex: 'error', ellipsis: true },
  { title: t('traffic.requestId'), key: 'request_id', dataIndex: 'request_id', ellipsis: true },
])

onMounted(async () => {
  try {
    servers.value = await listServers()
  } catch {
    servers.value = []
  }
  await load()
})

async function load() {
  try {
    const [logList, trendRes] = await Promise.all([
      getLogs({ server_id: serverId.value || undefined, limit: 500 }),
      getMetricsTrend('tool', 30),
    ])
    logs.value = logList
    trendData.value = trendRes.series
  } catch (e) {
    message.error(String(e))
  }
}
</script>

<style scoped>
.filters {
  margin-bottom: 12px;
}
.hint {
  color: #999;
  font-size: 12px;
}
.err-cell {
  color: #cf1322;
  font-size: 12px;
}
.trend-block {
  margin-bottom: 12px;
}
.trend-head {
  font-size: 12px;
  color: var(--mc-ink-2);
  letter-spacing: 0.06em;
  margin-bottom: 4px;
}
</style>
