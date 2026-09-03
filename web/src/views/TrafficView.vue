<template>
  <a-card :bordered="true">
    <template #title>{{ t('page.traffic') }}</template>
    <template #extra>
      <a-button size="small" @click="load">
        <template #icon><ReloadOutlined /></template>{{ t('traffic.refresh') }}
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
import { computed, onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { getLogs } from '@/api'
import type { TrafficSample } from '@/types'

const { t } = useI18n()
const logs = ref<TrafficSample[]>([])

const columns = computed<any[]>(() => [
  { title: t('traffic.time'), key: 'timestamp', dataIndex: 'timestamp', width: 190 },
  { title: t('traffic.tool'), key: 'tool', dataIndex: 'tool' },
  { title: t('traffic.server'), key: 'server_id', dataIndex: 'server_id', width: 120 },
  { title: t('traffic.client'), key: 'client', dataIndex: 'client', width: 120 },
  { title: t('traffic.status'), key: 'status', dataIndex: 'status', width: 110 },
  { title: t('traffic.latencyMs'), key: 'latency_ms', dataIndex: 'latency_ms', width: 110 },
  { title: t('traffic.requestId'), key: 'request_id', dataIndex: 'request_id', ellipsis: true },
])

onMounted(load)

async function load() {
  try {
    logs.value = await getLogs()
  } catch (e) {
    message.error(String(e))
  }
}
</script>