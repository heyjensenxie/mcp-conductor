<template>
  <a-row :gutter="[16, 16]">
    <a-col v-for="card in cards" :key="card.key" :xs="12" :md="8" :xl="4">
      <a-card class="stat-card" :bordered="true">
        <div class="stat-label">{{ t(`dashboard.${card.key}`) }}</div>
        <div class="stat-value">{{ card.value }}</div>
      </a-card>
    </a-col>
  </a-row>

  <a-row :gutter="[16, 16]" class="row-gap">
    <a-col :span="14">
      <a-card :title="t('dashboard.recentCalls')" :bordered="true">
        <a-table :data-source="logs" :columns="logColumns" :pagination="false" size="small">
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'status'">
              <a-tag :color="record.status === 'success' ? 'green' : 'red'">{{ record.status }}</a-tag>
            </template>
          </template>
        </a-table>
      </a-card>
    </a-col>
    <a-col :span="10">
      <a-card :title="t('dashboard.serverHealth')" :bordered="true">
        <a-table :data-source="servers" :columns="healthColumns" :pagination="false" size="small">
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'health'">
              <a-tag :color="record.health_status === 'healthy' ? 'green' : record.health_status === 'unhealthy' ? 'red' : 'default'">
                {{ record.health_status }}
              </a-tag>
            </template>
            <template v-else-if="column.key === 'tools'">{{ toolCount[record.id] ?? 0 }}</template>
          </template>
        </a-table>
      </a-card>
    </a-col>
  </a-row>

  <a-card :title="t('dashboard.topTools')" :bordered="true" class="row-gap">
    <TopToolsChart :data="topTools" />
  </a-card>
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
  { key: 'registeredServers', value: 0 as number | string },
  { key: 'healthyServers', value: 0 },
  { key: 'availableTools', value: 0 },
  { key: 'requests', value: 0 },
  { key: 'successRate', value: '-' },
  { key: 'p95Latency', value: '-' },
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
    // 后端不可达时保留空态，避免整页报错。
    console.error('load dashboard failed', e)
  }
})
</script>

<style scoped>
.stat-card {
  text-align: center;
}
.stat-label {
  color: rgba(0, 0, 0, 0.45);
  font-size: 13px;
}
.stat-value {
  font-size: 26px;
  font-weight: 600;
  margin-top: 6px;
}
.row-gap {
  margin-top: 16px;
}
</style>