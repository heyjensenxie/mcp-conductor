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
        <a-select v-model:value="logFilters.serverId" allow-clear show-search option-filter-prop="label" :placeholder="t('filter.serverPlaceholder')" style="width: 200px" @change="onServerFilter">
          <a-select-option v-for="s in servers" :key="s.id" :value="s.id" :label="s.name">{{ s.name }}</a-select-option>
        </a-select>
        <a-select v-if="instances.length > 1" v-model:value="logFilters.instanceId" allow-clear :placeholder="t('traffic.filterInstance')" style="width: 200px" @change="onFilterChange">
          <a-select-option v-for="inst in instances" :key="inst.id" :value="inst.id">{{ inst.id }}</a-select-option>
        </a-select>
        <a-input v-model:value="logFilters.q" allow-clear :placeholder="t('filter.keyword')" style="width: 200px" @press-enter="onFilterChange">
          <template #prefix><SearchOutlined /></template>
        </a-input>
        <a-select v-model:value="logFilters.status" allow-clear :placeholder="t('filter.statusPlaceholder')" style="width: 170px" @change="onFilterChange">
          <a-select-option value="success">{{ t('traffic.statusSuccess') }}</a-select-option>
          <a-select-option v-for="code in statusOptions" :key="code" :value="code">{{ code }}</a-select-option>
        </a-select>
        <a-range-picker v-model:value="dateRange" show-time class="range" @change="onRangeChange" />
      </a-space>
    </div>

    <a-table
      :data-source="logs"
      :columns="columns"
      size="small"
      :loading="loading"
      :pagination="pagination"
      :row-key="(r: any) => r.request_id"
      @change="onTableChange"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'status'">
          <a-tag :color="record.status === 'success' ? 'green' : 'red'">{{ record.status }}</a-tag>
        </template>
        <template v-else-if="column.key === 'instance_id'">
          <span v-if="record.instance_id" class="mono">{{ record.instance_id }}</span>
          <span v-else>-</span>
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
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { ReloadOutlined, SearchOutlined } from '@ant-design/icons-vue'
import { getLogs, getMetricsTrend, listAllServers, listServerInstances } from '@/api'
import type { MCPServer, ServerInstance, TrafficSample, TrendPoint } from '@/types'
import TrafficTrend from '@/components/TrafficTrend.vue'

const { t } = useI18n()
const logs = ref<TrafficSample[]>([])
const servers = ref<MCPServer[]>([])
const instances = ref<ServerInstance[]>([])
const loading = ref(false)
const trendData = ref<TrendPoint[]>([])

// 服务端分页：total 来自后端匹配总数；筛选条件变化回第 1 页。
const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => t('filter.total', { total }),
})
const logFilters = reactive({ serverId: '', instanceId: '', q: '', status: '', from: '', to: '' })
const dateRange = ref<any>(null)

// 失败状态=标准错误码；'success' 之外的 code 直接展示。
const statusOptions = [
  'protocol_error',
  'authentication_error',
  'authorization_error',
  'rate_limit_error',
  'route_error',
  'upstream_error',
  'timeout_error',
  'invalid_argument',
  'not_found',
  'internal_error',
]

function fmtMin(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

const trendSeries = computed(() => ({
  categories: trendData.value.map((p) => fmtMin(p.ts)),
  totals: trendData.value.map((p) => p.totals),
  rates: trendData.value.map((p) => (p.totals ? Math.round(((p.totals - p.errors) / p.totals) * 100) : 0)),
}))
const trendHasData = computed(() => trendData.value.some((p) => p.totals > 0))

const columns = computed<any[]>(() => [
  { title: t('traffic.time'), key: 'timestamp', dataIndex: 'timestamp', width: 190 },
  { title: t('traffic.tool'), key: 'tool', dataIndex: 'tool' },
  { title: t('traffic.server'), key: 'server_id', dataIndex: 'server_id', width: 90 },
  { title: t('traffic.instance'), key: 'instance_id', dataIndex: 'instance_id', width: 110 },
  { title: t('traffic.client'), key: 'client', dataIndex: 'client', width: 100 },
  { title: t('traffic.status'), key: 'status', dataIndex: 'status', width: 100 },
  { title: t('traffic.latencyMs'), key: 'latency_ms', dataIndex: 'latency_ms', width: 100 },
  { title: t('traffic.error'), key: 'error', dataIndex: 'error', ellipsis: true },
  { title: t('traffic.requestId'), key: 'request_id', dataIndex: 'request_id', ellipsis: true },
])

onMounted(async () => {
  try {
    servers.value = await listAllServers()
  } catch {
    servers.value = []
  }
  await load()
})

async function load() {
  loading.value = true
  try {
    const [logRes, trendRes] = await Promise.all([
      getLogs({
        server_id: logFilters.serverId || undefined,
        instance_id: logFilters.instanceId || undefined,
        q: logFilters.q.trim() || undefined,
        status: logFilters.status || undefined,
        from: logFilters.from || undefined,
        to: logFilters.to || undefined,
        page: pagination.current,
        page_size: pagination.pageSize,
      }),
      getMetricsTrend('tool', 30),
    ])
    logs.value = logRes.items
    pagination.total = logRes.total
    trendData.value = trendRes.series
    if (logRes.items.length === 0 && pagination.current > 1 && logRes.total > 0) {
      pagination.current -= 1
      await load()
      return
    }
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}

function onFilterChange() {
  pagination.current = 1
  void load()
}

// 切换 Server 时级联加载其实例，供按实例筛选；实例下拉仅在多实例 Server 出现。
async function onServerFilter() {
  instances.value = []
  logFilters.instanceId = ''
  if (logFilters.serverId) {
    try {
      instances.value = await listServerInstances(logFilters.serverId)
    } catch {
      instances.value = []
    }
  }
  onFilterChange()
}

function onTableChange(p: { current?: number; pageSize?: number }) {
  if (p.current) pagination.current = p.current
  if (p.pageSize && p.pageSize !== pagination.pageSize) {
    pagination.pageSize = p.pageSize
    pagination.current = 1
  }
  void load()
}

// 时间范围 → RFC3339（含时区偏移），后端转 UTC 做闭区间过滤。
function onRangeChange(dates: any) {
  if (dates && dates[0] && dates[1]) {
    logFilters.from = dates[0].format('YYYY-MM-DDTHH:mm:ssZ')
    logFilters.to = dates[1].format('YYYY-MM-DDTHH:mm:ssZ')
  } else {
    logFilters.from = ''
    logFilters.to = ''
  }
  onFilterChange()
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
.range {
  min-width: 320px;
}
.mono {
  font-family: var(--mc-mono);
}
</style>
