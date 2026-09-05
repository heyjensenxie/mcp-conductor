<template>
  <a-card :bordered="true">
    <template #title>{{ t('page.observability') }}</template>
    <template #extra>
      <a-space :size="8">
        <a-radio-group v-model:value="scope" size="small" button-style="solid" @change="load">
          <a-radio-button value="tool">{{ t('observability.scopeTool') }}</a-radio-button>
          <a-radio-button value="server">{{ t('observability.scopeServer') }}</a-radio-button>
        </a-radio-group>
        <a-button size="small" @click="load">
          <template #icon><ReloadOutlined /></template>{{ t('observability.refresh') }}
        </a-button>
      </a-space>
    </template>

    <div v-if="hasTrend" class="trend-block">
      <div class="trend-head mono">{{ t('observability.scopeTrendTitle') }}</div>
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
import { getMetrics, getServerMetrics, getMetricsTrend } from '@/api'
import type { MetricSnapshot, TrendPoint } from '@/types'
import TrafficTrend from '@/components/TrafficTrend.vue'

const { t } = useI18n()
const scope = ref<'tool' | 'server'>('tool')
const metrics = ref<MetricSnapshot[]>([])
const trendData = ref<TrendPoint[]>([])
const loading = ref(false)

function fmtMin(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// 当前 scope 的真时序折线（近 30 分钟）。
const trendSeries = computed(() => ({
  categories: trendData.value.map((p) => fmtMin(p.ts)),
  totals: trendData.value.map((p) => p.totals),
  rates: trendData.value.map((p) => (p.totals ? Math.round(((p.totals - p.errors) / p.totals) * 100) : 0)),
}))
const hasTrend = computed(() => trendData.value.some((p) => p.totals > 0))

// 按当前维度展示：Tool 显示工具维；Server 去掉 server: 前缀显示 Server id。
const rows = computed<MetricSnapshot[]>(() =>
  metrics.value.map((m) =>
    scope.value === 'server' && m.key.startsWith('server:')
      ? { ...m, key: m.key.slice('server:'.length) }
      : m,
  ),
)

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

onMounted(load)

async function load() {
  loading.value = true
  try {
    const [snapshots, trendRes] = await Promise.all([
      scope.value === 'server' ? getServerMetrics() : getMetrics(),
      getMetricsTrend(scope.value, 30),
    ])
    metrics.value = snapshots
    trendData.value = trendRes.series
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}
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
