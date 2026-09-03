<template>
  <a-card :bordered="true" title="Traffic">
    <template #extra>
      <a-button size="small" @click="load">
        <template #icon><ReloadOutlined /></template>Refresh
      </a-button>
    </template>
    <a-table :data-source="logs" :columns="columns" :pagination="false" size="small" :row-key="(r: any) => r.request_id">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'status'">
          <a-tag :color="record.status === 'success' ? 'green' : 'red'">{{ record.status }}</a-tag>
        </template>
      </template>
    </a-table>
  </a-card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { getLogs } from '@/api'
import type { TrafficSample } from '@/types'

const logs = ref<TrafficSample[]>([])
const columns: any[] = [
  { title: 'Time', key: 'timestamp', dataIndex: 'timestamp', width: 190 },
  { title: 'Tool', key: 'tool', dataIndex: 'tool' },
  { title: 'Server', key: 'server_id', dataIndex: 'server_id', width: 110 },
  { title: 'Client', key: 'client', dataIndex: 'client', width: 110 },
  { title: 'Status', key: 'status', dataIndex: 'status', width: 110 },
  { title: 'Latency (ms)', key: 'latency_ms', dataIndex: 'latency_ms', width: 110 },
  { title: 'Request ID', key: 'request_id', dataIndex: 'request_id', ellipsis: true },
]

onMounted(load)

async function load() {
  try {
    logs.value = await getLogs()
  } catch (e) {
    message.error(String(e))
  }
}
</script>