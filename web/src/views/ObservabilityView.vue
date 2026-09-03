<template>
  <a-card :bordered="true" title="Observability">
    <template #extra>
      <a-button size="small" @click="load">
        <template #icon><ReloadOutlined /></template>Refresh
      </a-button>
    </template>
    <a-table :data-source="metrics" :columns="columns" :loading="loading" :pagination="false" :row-key="(r: any) => r.key">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'success_rate'">{{ (record.success_rate * 100).toFixed(1) }}%</template>
      </template>
    </a-table>
  </a-card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { getMetrics } from '@/api'
import type { MetricSnapshot } from '@/types'

const metrics = ref<MetricSnapshot[]>([])
const loading = ref(false)
const columns: any[] = [
  { title: 'Dimension', key: 'key', dataIndex: 'key' },
  { title: 'Requests', key: 'totals', dataIndex: 'totals', width: 100 },
  { title: 'Success', key: 'success', dataIndex: 'success', width: 100 },
  { title: 'Errors', key: 'errors', dataIndex: 'errors', width: 100 },
  { title: 'Success Rate', key: 'success_rate', width: 120 },
  { title: 'P50 (ms)', key: 'p50', dataIndex: 'p50', width: 100 },
  { title: 'P95 (ms)', key: 'p95', dataIndex: 'p95', width: 100 },
  { title: 'P99 (ms)', key: 'p99', dataIndex: 'p99', width: 100 },
]

onMounted(load)

async function load() {
  loading.value = true
  try {
    metrics.value = await getMetrics()
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}
</script>