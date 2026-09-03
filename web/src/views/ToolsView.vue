<template>
  <a-card :bordered="true">
    <a-table :data-source="tools" :columns="columns" :loading="loading" :pagination="false" :row-key="(r: any) => r.id">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'enabled'">
          <a-tag :color="record.enabled ? 'green' : 'default'">{{ record.enabled ? 'yes' : 'no' }}</a-tag>
        </template>
      </template>
    </a-table>
  </a-card>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { listTools } from '@/api'
import type { Tool } from '@/types'

const tools = ref<Tool[]>([])
const loading = ref(false)
const columns: any[] = [
  { title: 'Gateway Name', key: 'gateway_name', dataIndex: 'gateway_name' },
  { title: 'Original', key: 'original_name', dataIndex: 'original_name', width: 150 },
  { title: 'Server', key: 'server_id', dataIndex: 'server_id', width: 130 },
  { title: 'Description', key: 'description', dataIndex: 'description', ellipsis: true },
  { title: 'Enabled', key: 'enabled', dataIndex: 'enabled', width: 90 },
]

onMounted(async () => {
  loading.value = true
  try {
    tools.value = await listTools()
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
})
</script>