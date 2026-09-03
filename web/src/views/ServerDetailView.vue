<template>
  <a-spin :spinning="loading">
    <a-page-header title="Back" @back="$router.push('/servers')" class="ph">
      <template #subTitle>{{ server?.name }}</template>
      <template #tags>
        <a-tag :color="healthColor(server?.health_status)">{{ server?.health_status }}</a-tag>
      </template>
    </a-page-header>

    <a-descriptions v-if="server" :column="2" bordered size="small" class="mb">
      <a-descriptions-item label="ID">{{ server.id }}</a-descriptions-item>
      <a-descriptions-item label="Status">
        <a-tag :color="server.enabled ? 'green' : 'default'">{{ server.enabled ? 'Enabled' : 'Disabled' }}</a-tag>
      </a-descriptions-item>
      <a-descriptions-item label="Endpoint" :span="1">{{ server.endpoint }}</a-descriptions-item>
      <a-descriptions-item label="Transport">{{ server.transport }}</a-descriptions-item>
      <a-descriptions-item label="Description" :span="2">{{ server.description || '-' }}</a-descriptions-item>
    </a-descriptions>

    <a-space class="mb">
      <a-button @click="test">Test Connection</a-button>
      <a-button v-if="server" :danger="server.enabled" @click="toggle">{{ server.enabled ? 'Disable' : 'Enable' }}</a-button>
    </a-space>

    <a-tabs v-model:activeKey="tab">
      <a-tab-pane key="tools" tab="Tools">
        <a-table :data-source="tools" :columns="toolColumns" :pagination="false" size="small" :row-key="(r: any) => r.id" />
      </a-tab-pane>
      <a-tab-pane key="credentials" tab="Credentials">
        <a-table :data-source="credentials" :columns="credColumns" :pagination="false" size="small" :row-key="(r: any) => r.id">
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'has_value'">
              <a-tag :color="record.has_value ? 'green' : 'default'">{{ record.has_value ? 'configured' : 'empty' }}</a-tag>
            </template>
          </template>
        </a-table>
      </a-tab-pane>
    </a-tabs>
  </a-spin>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { message } from 'ant-design-vue'
import { getServer, listServerCredentials, listServerTools, testServer, toggleServer } from '@/api'
import type { Credential, MCPServer, Tool } from '@/types'

const route = useRoute()
const id = computed(() => String(route.params.id))
const server = ref<MCPServer>()
const tools = ref<Tool[]>([])
const credentials = ref<Credential[]>([])
const loading = ref(false)
const tab = ref('tools')

const toolColumns: any[] = [
  { title: 'Gateway Name', key: 'gateway_name', dataIndex: 'gateway_name' },
  { title: 'Original', key: 'original_name', dataIndex: 'original_name', width: 140 },
  { title: 'Description', key: 'description', dataIndex: 'description', ellipsis: true },
]
const credColumns: any[] = [
  { title: 'Name', key: 'name', dataIndex: 'name' },
  { title: 'Kind', key: 'kind', dataIndex: 'kind', width: 140 },
  { title: 'Header', key: 'header', dataIndex: 'header', width: 160 },
  { title: 'Value', key: 'has_value', width: 100 },
]

onMounted(load)

async function load() {
  loading.value = true
  try {
    server.value = await getServer(id.value)
    tools.value = await listServerTools(id.value)
    credentials.value = await listServerCredentials(id.value)
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}

async function test() {
  try {
    await testServer(id.value)
    message.success('测试连接通过')
    await load()
  } catch (e) {
    message.error(`连接失败: ${e}`)
  }
}

async function toggle() {
  if (!server.value) return
  try {
    await toggleServer(id.value, !server.value.enabled)
    await load()
  } catch (e) {
    message.error(String(e))
  }
}

const healthColor = (s?: string) => (s === 'healthy' ? 'green' : s === 'unhealthy' ? 'red' : 'default')
</script>

<style scoped>
.ph {
  padding: 0 0 4px;
}
.mb {
  margin-bottom: 16px;
}
</style>