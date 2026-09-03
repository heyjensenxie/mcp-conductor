<template>
  <a-row :gutter="[16, 16]">
    <a-col v-for="card in cards" :key="card.label" :xs="12" :md="8" :xl="4">
      <a-card class="stat-card" :bordered="true">
        <div class="stat-label">{{ card.label }}</div>
        <div class="stat-value">{{ card.value }}</div>
      </a-card>
    </a-col>
  </a-row>

  <a-row :gutter="[16, 16]" class="row-gap">
    <a-col :span="14">
      <a-card title="Recent Tool Calls" :bordered="true">
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
      <a-card title="Server Health" :bordered="true">
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

  <a-card title="Top Tools by Calls" :bordered="true" class="row-gap">
    <TopToolsChart :data="topTools" />
  </a-card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { listServers, listServerTools, getLogs, getMetrics } from '@/api'
import type { MCPServer, TrafficSample, MetricSnapshot } from '@/types'
import TopToolsChart, { type ChartDatum } from '@/components/TopToolsChart.vue'

const servers = ref<MCPServer[]>([])
const logs = ref<TrafficSample[]>([])
const metrics = ref<MetricSnapshot[]>([])
const toolCount = ref<Record<string, number>>({})
const cards = ref([
  { label: 'Registered Servers', value: 0 as number | string },
  { label: 'Healthy', value: 0 },
  { label: 'Available Tools', value: 0 },
  { label: 'Requests', value: 0 },
  { label: 'Success Rate', value: '-' },
  { label: 'P95 Latency', value: '-' },
])

const topTools = ref<ChartDatum[]>([])

const logColumns: any[] = [
  { title: 'Tool', key: 'tool', dataIndex: 'tool' },
  { title: 'Status', key: 'status', dataIndex: 'status' },
  { title: 'Latency (ms)', key: 'latency', dataIndex: 'latency_ms' },
  { title: 'Client', key: 'client', dataIndex: 'client' },
  { title: 'Request ID', key: 'request_id', dataIndex: 'request_id', ellipsis: true },
]

const healthColumns: any[] = [
  { title: 'Server', key: 'name', dataIndex: 'name' },
  { title: 'Health', key: 'health', dataIndex: 'health_status' },
  { title: 'Tools', key: 'tools' },
]

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
    console.error('加载 Dashboard 失败', e)
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
.stat-hint {
  color: rgba(0, 0, 0, 0.45);
  font-size: 12px;
  margin-top: 4px;
}
.row-gap {
  margin-top: 16px;
}
</style>