<template>
  <div>
    <div class="toolbar">
      <a-space>
        <a-button type="primary" @click="openCreate">
          <template #icon><PlusOutlined /></template>Register Server
        </a-button>
        <a-button @click="load">
          <template #icon><ReloadOutlined /></template>Refresh
        </a-button>
      </a-space>
    </div>

    <a-card :bordered="true">
      <a-table :data-source="servers" :columns="columns" :loading="loading" :row-key="(r: any) => r.id" :pagination="false">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <router-link :to="`/servers/${record.id}`" class="link">{{ record.name }}</router-link>
          </template>
          <template v-else-if="column.key === 'health'">
            <a-tag :color="healthColor(record.health_status)" :bordered="false">{{ record.health_status }}</a-tag>
          </template>
          <template v-else-if="column.key === 'enabled'">
            <a-tag :color="record.enabled ? 'green' : 'default'">{{ record.enabled ? 'Enabled' : 'Disabled' }}</a-tag>
          </template>
          <template v-else-if="column.key === 'tools'">{{ toolCount[record.id] ?? 0 }}</template>
          <template v-else-if="column.key === 'actions'">
            <a-space :size="4">
              <a-button size="small" @click="test(record)">Test</a-button>
              <a-button size="small" :danger="record.enabled" @click="toggle(record)">
                {{ record.enabled ? 'Disable' : 'Enable' }}
              </a-button>
              <a-popconfirm title="确认删除该 Server（含其 Tools）？" @confirm="remove(record)">
                <a-button size="small" danger>Delete</a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal v-model:open="dialogVisible" title="Register MCP Server" :confirm-loading="submitting" @ok="submit" ok-text="Register" cancel-text="Cancel">
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item label="Name" :required="true">
          <a-input v-model:value="form.name" placeholder="如 University MCP" />
        </a-form-item>
        <a-form-item label="Endpoint" :required="true">
          <a-input v-model:value="form.endpoint" placeholder="如 http://localhost:9000/mcp" />
        </a-form-item>
        <a-form-item label="Transport">
          <a-select v-model:value="form.transport">
            <a-select-option value="https">streamable HTTP</a-select-option>
            <a-select-option value="sse">SSE</a-select-option>
            <a-select-option value="stdio">stdio</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="Description">
          <a-textarea v-model:value="form.description" :rows="2" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { createServer, deleteServer, listServers, listServerTools, testServer, toggleServer } from '@/api'
import type { MCPServer, Transport } from '@/types'

const servers = ref<MCPServer[]>([])
const toolCount = ref<Record<string, number>>({})
const loading = ref(false)
const dialogVisible = ref(false)
const submitting = ref(false)
const form = reactive<{ name: string; endpoint: string; transport: Transport; description: string }>({
  name: '',
  endpoint: '',
  transport: 'https',
  description: '',
})

const columns: any[] = [
  { title: 'Name', key: 'name', dataIndex: 'name' },
  { title: 'Endpoint', key: 'endpoint', dataIndex: 'endpoint', ellipsis: true },
  { title: 'Transport', key: 'transport', dataIndex: 'transport', width: 100 },
  { title: 'Tools', key: 'tools', width: 70 },
  { title: 'Health', key: 'health', dataIndex: 'health_status', width: 100 },
  { title: 'Status', key: 'enabled', dataIndex: 'enabled', width: 100 },
  { title: 'Actions', key: 'actions', width: 230 },
]

onMounted(load)

async function load() {
  loading.value = true
  try {
    servers.value = await listServers()
    for (const s of servers.value) {
      try {
        toolCount.value[s.id] = (await listServerTools(s.id)).length
      } catch {
        toolCount.value[s.id] = 0
      }
    }
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  form.name = ''
  form.endpoint = ''
  form.transport = 'https'
  form.description = ''
  dialogVisible.value = true
}

async function submit() {
  if (!form.name || !form.endpoint) {
    message.warning('Name 与 Endpoint 必填')
    return
  }
  submitting.value = true
  try {
    await createServer({ ...form })
    message.success('Server 已注册，正在后台发现 Tools')
    dialogVisible.value = false
    await load()
  } catch (e) {
    message.error(String(e))
  } finally {
    submitting.value = false
  }
}

async function toggle(row: MCPServer) {
  try {
    await toggleServer(row.id, !row.enabled)
    await load()
  } catch (e) {
    message.error(String(e))
  }
}

async function test(row: MCPServer) {
  try {
    await testServer(row.id)
    message.success('测试连接通过，Tools 已刷新')
    await load()
  } catch (e) {
    message.error(`连接失败: ${e}`)
  }
}

async function remove(row: MCPServer) {
  try {
    await deleteServer(row.id)
    message.success('已删除')
    await load()
  } catch (e) {
    message.error(String(e))
  }
}

const healthColor = (s: string) => (s === 'healthy' ? 'green' : s === 'unhealthy' ? 'red' : 'default')
</script>

<style scoped>
.toolbar {
  margin-bottom: 16px;
}
.link {
  color: #1677ff;
  font-weight: 500;
}
</style>