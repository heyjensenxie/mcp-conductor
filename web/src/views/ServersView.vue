<template>
  <div>
    <div class="toolbar">
      <a-space>
        <a-button type="primary" @click="openCreate">
          <template #icon><PlusOutlined /></template>{{ t('servers.register') }}
        </a-button>
        <a-button @click="load">
          <template #icon><ReloadOutlined /></template>{{ t('servers.refresh') }}
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
            <a-tag :color="record.enabled ? 'green' : 'default'">{{ record.enabled ? t('servers.enabled') : t('servers.disabled') }}</a-tag>
          </template>
          <template v-else-if="column.key === 'tools'">{{ toolCount[record.id] ?? 0 }}</template>
          <template v-else-if="column.key === 'actions'">
            <a-space :size="4">
              <a-button size="small" @click="test(record)">{{ t('servers.test') }}</a-button>
              <a-button size="small" :danger="record.enabled" @click="toggle(record)">
                {{ record.enabled ? t('servers.disable') : t('servers.enable') }}
              </a-button>
              <a-popconfirm :title="t('servers.confirmDelete')" @confirm="remove(record)">
                <a-button size="small" danger>{{ t('servers.delete') }}</a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal v-model:open="dialogVisible" :title="t('servers.dialogTitle')" :confirm-loading="submitting" @ok="submit" :ok-text="t('servers.register')" :cancel-text="t('common.cancel')">
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item :label="t('common.name')" :required="true">
          <a-input v-model:value="form.name" placeholder="University MCP" />
        </a-form-item>
        <a-form-item :label="t('servers.endpoint')" :required="true">
          <a-input v-model:value="form.endpoint" :placeholder="t('servers.endpointPlaceholder')" />
        </a-form-item>
        <a-form-item :label="t('servers.transport')">
          <a-select v-model:value="form.transport">
            <a-select-option value="https">{{ t('servers.transportStreamable') }}</a-select-option>
            <a-select-option value="sse">{{ t('servers.transportSSE') }}</a-select-option>
            <a-select-option value="stdio">{{ t('servers.transportStdio') }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="t('common.description')">
          <a-textarea v-model:value="form.description" :rows="2" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { createServer, deleteServer, listServers, listServerTools, testServer, toggleServer } from '@/api'
import type { MCPServer, Transport } from '@/types'

const { t } = useI18n()

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

const columns = computed<any[]>(() => [
  { title: t('common.name'), key: 'name', dataIndex: 'name' },
  { title: t('servers.endpoint'), key: 'endpoint', dataIndex: 'endpoint', ellipsis: true },
  { title: t('servers.transport'), key: 'transport', dataIndex: 'transport', width: 110 },
  { title: t('servers.tools'), key: 'tools', width: 70 },
  { title: t('servers.health'), key: 'health', dataIndex: 'health_status', width: 100 },
  { title: t('common.status'), key: 'enabled', dataIndex: 'enabled', width: 100 },
  { title: t('common.actions'), key: 'actions', width: 240 },
])

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
    message.warning(t('servers.fillRequired'))
    return
  }
  submitting.value = true
  try {
    await createServer({ ...form })
    message.success(t('servers.registeredOk'))
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
    message.success(t('servers.testOk'))
    await load()
  } catch (e) {
    message.error(`${t('servers.connectFail')}: ${e}`)
  }
}

async function remove(row: MCPServer) {
  try {
    await deleteServer(row.id)
    message.success(t('servers.deletedOk'))
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