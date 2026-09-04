<template>
  <div class="dash">
    <!-- 统计卡：错落入场 + 悬浮抬升 -->
    <a-row :gutter="[16, 16]">
      <a-col v-for="(card, i) in cards" :key="card.key" :xs="12" :md="8" :xl="4" class="reveal" :style="{ animationDelay: `${i * 60}ms` }">
        <div class="stat-card mc-panel">
          <div class="stat-top">
            <span class="stat-label mono">{{ t(`dashboard.${card.key}`) }}</span>
            <span class="mc-dot" :class="`mc-dot--${card.dot}`"></span>
          </div>
          <div class="stat-value mono">{{ card.value }}</div>
        </div>
      </a-col>
    </a-row>

    <a-row :gutter="[16, 16]" class="row-gap reveal" :style="{ animationDelay: '180ms' }">
      <a-col :span="14">
        <section class="mc-panel panel">
          <header class="panel-head mono">{{ t('dashboard.recentCalls') }}</header>
          <a-table :data-source="logs" :columns="logColumns" :pagination="false" size="small">
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'status'">
                <span class="mc-dot" :class="record.status === 'success' ? 'mc-dot--ok' : 'mc-dot--bad'"></span>
                <span class="cell-status">{{ record.status }}</span>
              </template>
            </template>
          </a-table>
        </section>
      </a-col>
      <a-col :span="10">
        <section class="mc-panel panel">
          <header class="panel-head mono">{{ t('dashboard.serverHealth') }}</header>
          <a-table :data-source="servers" :columns="healthColumns" :pagination="false" size="small">
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'health'">
                <span class="mc-dot" :class="dotClass(record.health_status)"></span>
                <span class="cell-status">{{ record.health_status }}</span>
              </template>
              <template v-else-if="column.key === 'tools'">
                <span class="mono">{{ toolCount[record.id] ?? 0 }}</span>
              </template>
            </template>
          </a-table>
        </section>
      </a-col>
    </a-row>

    <section class="mc-panel panel row-gap reveal" :style="{ animationDelay: '240ms' }">
      <header class="panel-head mono">{{ t('dashboard.topTools') }}</header>
      <TopToolsChart :data="topTools" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listServers, listServerTools, getLogs, getMetrics } from '@/api'
import type { MCPServer, TrafficSample, MetricSnapshot } from '@/types'
import TopToolsChart, { type ChartDatum } from '@/components/TopToolsChart.vue'

const { t } = useI18n()

const servers = ref<MCPServer[]>([])
const logs = ref<TrafficSample[]>([])
const metrics = ref<MetricSnapshot[]>([])
const toolCount = ref<Record<string, number>>({})
const cards = ref([
  { key: 'registeredServers', value: 0 as number | string, dot: 'idle' },
  { key: 'healthyServers', value: 0, dot: 'ok' },
  { key: 'availableTools', value: 0, dot: 'idle' },
  { key: 'requests', value: 0, dot: 'idle' },
  { key: 'successRate', value: '-', dot: 'ok' },
  { key: 'p95Latency', value: '-', dot: 'warn' },
])

const topTools = ref<ChartDatum[]>([])

const logColumns = computed<any[]>(() => [
  { title: t('traffic.tool'), key: 'tool', dataIndex: 'tool' },
  { title: t('traffic.status'), key: 'status', dataIndex: 'status' },
  { title: t('traffic.latencyMs'), key: 'latency', dataIndex: 'latency_ms' },
  { title: t('traffic.client'), key: 'client', dataIndex: 'client' },
  { title: t('traffic.requestId'), key: 'request_id', dataIndex: 'request_id', ellipsis: true },
])

const healthColumns = computed<any[]>(() => [
  { title: t('traffic.server'), key: 'name', dataIndex: 'name' },
  { title: t('dashboard.serverHealth'), key: 'health', dataIndex: 'health_status' },
  { title: t('dashboard.availableTools'), key: 'tools', width: 90 },
])

const dotClass = (s: string) => (s === 'healthy' ? 'mc-dot--ok' : s === 'unhealthy' ? 'mc-dot--bad' : s === 'disabled' ? 'mc-dot--idle' : 'mc-dot--warn')

onMounted(async () => {
  try {
    const [serverList, logList, metricList] = await Promise.all([listServers(), getLogs(), getMetrics()])
    servers.value = serverList
    logs.value = logList
    metrics.value = metricList

    let toolTotal = 0
    for (const s of serverList) {
      try {
        const tools = await listServerTools(s.id)
        toolCount.value[s.id] = tools.length
        toolTotal += tools.length
      } catch {
        toolCount.value[s.id] = 0
      }
    }

    const healthy = serverList.filter((s) => s.health_status === 'healthy').length
    cards.value[0].value = serverList.length
    cards.value[1].value = healthy
    cards.value[2].value = toolTotal
    cards.value[3].value = logList.length
    if (logList.length) {
      const ok = logList.filter((l) => l.status === 'success').length
      cards.value[4].value = `${Math.round((ok / logList.length) * 100)}%`
      const lat = logList.map((l) => l.latency_ms).sort((a, b) => a - b)
      cards.value[5].value = lat[Math.floor(lat.length * 0.95)] ?? 0
    }

    topTools.value = metricList
      .filter((m) => m.totals > 0)
      .sort((a, b) => b.totals - a.totals)
      .slice(0, 8)
      .map((m) => ({ name: m.key, value: Number(m.totals) }))
  } catch (e) {
    console.error('load dashboard failed', e)
  }
})
</script>

<style scoped>
.stat-card {
  position: relative;
  padding: 14px 18px 16px;
  overflow: hidden;
  transition: transform 0.18s ease, box-shadow 0.18s ease;
}
.stat-card::before {
  content: '';
  position: absolute;
  inset: 0 auto auto 0;
  width: 34px;
  height: 2px;
  border-radius: 2px;
  background: var(--mc-accent);
}
.stat-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--mc-shadow-raise);
}
.stat-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}
.stat-label {
  font-size: 11.5px;
  letter-spacing: 0.07em;
  color: var(--mc-ink-2);
  text-transform: uppercase;
}
.stat-value {
  font-size: 30px;
  font-weight: 600;
  line-height: 1.1;
  color: var(--mc-ink);
}

.panel {
  background: var(--mc-elev);
  padding: 16px 18px;
}
.panel-head {
  font-size: 12px;
  letter-spacing: 0.08em;
  color: var(--mc-ink-2);
  margin-bottom: 10px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.panel-head::before {
  content: '';
  width: 8px;
  height: 8px;
  border-radius: 2px;
  background: var(--mc-accent);
}
.cell-status {
  margin-left: 7px;
  font-size: 12.5px;
  color: var(--mc-ink-2);
}
.row-gap {
  margin-top: 16px;
}
</style>