<template>
  <a-card :bordered="true">
    <a-table :data-source="tools" :columns="columns" :loading="loading" :pagination="false" :row-key="(r: any) => r.id">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'actions'">
          <a-switch
            :checked="record.enabled"
            :loading="toggling === record.id"
            @change="(checked: boolean) => toggle(record, checked)"
          />
        </template>
      </template>
    </a-table>
  </a-card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { listTools, toggleTool } from '@/api'
import type { Tool } from '@/types'

const { t } = useI18n()
const tools = ref<Tool[]>([])
const loading = ref(false)
const toggling = ref('')

const columns = computed<any[]>(() => [
  { title: t('tools.gatewayName'), key: 'gateway_name', dataIndex: 'gateway_name' },
  { title: t('tools.original'), key: 'original_name', dataIndex: 'original_name', width: 150 },
  { title: t('tools.server'), key: 'server_id', dataIndex: 'server_id', width: 140 },
  { title: t('tools.description'), key: 'description', dataIndex: 'description', ellipsis: true },
  { title: t('common.actions'), key: 'actions', width: 90 },
])

onMounted(load)

async function load() {
  loading.value = true
  try {
    tools.value = await listTools()
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}

async function toggle(record: Tool, enabled: boolean) {
  toggling.value = record.id
  try {
    await toggleTool(record.id, enabled)
    message.success(t('tools.toggledOk'))
    await load()
  } catch (e) {
    message.error(String(e))
  } finally {
    toggling.value = ''
  }
}
</script>
