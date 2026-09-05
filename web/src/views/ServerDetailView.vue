<template>
  <a-spin :spinning="loading">
    <a-page-header :title="t('serverDetail.back')" @back="$router.push('/servers')" class="ph">
      <template #subTitle>{{ server?.name }}</template>
      <template #tags>
        <a-tag :color="healthColor(server?.health_status)">{{ server?.health_status }}</a-tag>
      </template>
    </a-page-header>

    <a-tabs v-model:activeKey="tab" type="card">
      <!-- Overview：核心信息 + 快捷操作 -->
      <a-tab-pane :key="'overview'" :tab="t('serverDetail.tabOverview')">
        <a-descriptions v-if="server" :column="2" bordered size="small" class="mb">
          <a-descriptions-item label="ID">{{ server.id }}</a-descriptions-item>
          <a-descriptions-item :label="t('serverDetail.status')">
            <a-tag :color="server.enabled ? 'green' : 'default'">{{ server.enabled ? t('servers.enabled') : t('servers.disabled') }}</a-tag>
          </a-descriptions-item>
          <a-descriptions-item :label="t('serverDetail.endpoint')">{{ server.endpoint }}</a-descriptions-item>
          <a-descriptions-item :label="t('serverDetail.transport')">{{ server.transport }}</a-descriptions-item>
          <a-descriptions-item :label="t('serverDetail.instancesCount')">{{ instanceCount }}</a-descriptions-item>
          <a-descriptions-item :label="t('common.updated')">{{ server.updated_at }}</a-descriptions-item>
          <a-descriptions-item :label="t('common.description')" :span="2">{{ server.description || '-' }}</a-descriptions-item>
        </a-descriptions>

        <a-space>
          <a-button @click="test">{{ t('serverDetail.testConnection') }}</a-button>
          <a-button v-if="server" :danger="server.enabled" @click="toggle">
            {{ server.enabled ? t('serverDetail.disable') : t('serverDetail.enable') }}
          </a-button>
        </a-space>
      </a-tab-pane>

      <!-- Tools -->
      <a-tab-pane :key="'tools'" :tab="t('serverDetail.tools')">
        <div class="filters">
          <a-input v-model:value="toolFilters.q" allow-clear :placeholder="t('filter.keyword')" style="width: 240px" @press-enter="onToolFilter">
            <template #prefix><SearchOutlined /></template>
          </a-input>
        </div>
        <a-table
          :data-source="toolRows"
          :columns="toolColumns"
          :loading="toolsLoading"
          :pagination="toolPaging"
          size="small"
          :row-key="(r: any) => r.id"
          @change="onToolTableChange"
        />
      </a-tab-pane>

      <!-- Instances：MVP 单实例承载，预留多实例 -->
      <a-tab-pane :key="'instances'" :tab="t('serverDetail.instances')">
        <a-table :data-source="instanceRows" :columns="instanceColumns" :pagination="false" size="small" :row-key="(r: any) => r.endpoint" />
        <a-alert type="info" :message="t('serverDetail.instancesNote')" banner class="mb" />
      </a-tab-pane>

      <!-- Testing：内联工具调用（需要全量工具集，独立于分页表） -->
      <a-tab-pane :key="'testing'" :tab="t('serverDetail.testing')">
        <div class="toolbar">
          <a-space>
            <a-button size="small" :loading="testing" @click="testAndReloadTools">
              <template #icon><ReloadOutlined /></template>{{ t('serverDetail.rediscover') }}
            </a-button>
          </a-space>
        </div>
        <ToolInvoker :tools="tools" />
      </a-tab-pane>

      <!-- Logs：该 Server 的调用记录 -->
      <a-tab-pane :key="'logs'" :tab="t('serverDetail.logs')">
        <div class="toolbar">
          <a-space>
            <a-button size="small" @click="loadLogsPage"><template #icon><ReloadOutlined /></template>{{ t('common.refresh') }}</a-button>
          </a-space>
        </div>
        <div class="filters">
          <a-space wrap :size="8">
            <a-input v-model:value="logFilters.q" allow-clear :placeholder="t('filter.keyword')" style="width: 220px" @press-enter="onLogFilter">
              <template #prefix><SearchOutlined /></template>
            </a-input>
            <a-select v-model:value="logFilters.status" allow-clear :placeholder="t('filter.statusPlaceholder')" style="width: 150px" @change="onLogFilter">
              <a-select-option value="success">{{ t('traffic.statusSuccess') }}</a-select-option>
              <a-select-option v-for="code in errorStatuses" :key="code" :value="code">{{ code }}</a-select-option>
            </a-select>
          </a-space>
        </div>
        <a-table
          :data-source="logs"
          :columns="logColumns"
          :loading="logsLoading"
          :pagination="logPaging"
          size="small"
          :row-key="(r: any) => r.request_id"
          @change="onLogTableChange"
        />
      </a-tab-pane>

      <!-- Configuration：Server 可编辑字段 + 凭证管理 -->
      <a-tab-pane :key="'configuration'" :tab="t('serverDetail.configuration')">
        <a-row :gutter="16">
          <a-col :span="12">
            <a-card :bordered="true" :title="t('serverDetail.configServer')" class="mb">
              <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
                <a-form-item :label="t('common.name')">
                  <a-input :value="server?.name" disabled />
                </a-form-item>
                <a-form-item :label="t('common.description')">
                  <a-textarea v-model:value="editForm.description" :rows="2" />
                </a-form-item>
                <a-form-item :label="t('servers.endpoint')">
                  <a-input v-model:value="editForm.endpoint" />
                </a-form-item>
                <a-form-item :label="t('servers.transport')">
                  <a-select v-model:value="editForm.transport">
                    <a-select-option value="https">{{ t('servers.transportStreamable') }}</a-select-option>
                    <a-select-option value="sse">{{ t('servers.transportSSE') }}</a-select-option>
                    <a-select-option value="stdio">{{ t('servers.transportStdio') }}</a-select-option>
                  </a-select>
                </a-form-item>
                <a-form-item :wrapper-col="{ offset: 6, span: 18 }">
                  <a-button type="primary" :loading="savingServer" @click="saveServerEdit">{{ t('serverDetail.saveServer') }}</a-button>
                </a-form-item>
              </a-form>
            </a-card>
          </a-col>
          <a-col :span="12">
            <a-card :bordered="true" :title="t('serverDetail.credentials')" class="mb">
              <div class="toolbar">
                <a-space wrap :size="8">
                  <a-button size="small" type="primary" @click="openCredential()">
                    <template #icon><PlusOutlined /></template>{{ t('serverDetail.newCredential') }}
                  </a-button>
                  <a-input v-model:value="credFilters.q" allow-clear :placeholder="t('filter.keyword')" style="width: 160px" @press-enter="onCredFilter">
                    <template #prefix><SearchOutlined /></template>
                  </a-input>
                  <a-select v-model:value="credFilters.kind" allow-clear :placeholder="t('filter.kindPlaceholder')" style="width: 130px" @change="onCredFilter">
                    <a-select-option value="api_key">{{ t('serverDetail.kindApiKey') }}</a-select-option>
                    <a-select-option value="static_token">{{ t('serverDetail.kindStaticToken') }}</a-select-option>
                  </a-select>
                  <a-select v-model:value="credFilters.hasValue" allow-clear :placeholder="t('filter.hasValuePlaceholder')" style="width: 130px" @change="onCredFilter">
                    <a-select-option value="true">{{ t('filter.hasValue') }}</a-select-option>
                    <a-select-option value="false">{{ t('filter.noValue') }}</a-select-option>
                  </a-select>
                </a-space>
              </div>
              <a-table
                :data-source="credentials"
                :columns="credColumns"
                :loading="credsLoading"
                :pagination="credPaging"
                size="small"
                :row-key="(r: any) => r.id"
                @change="onCredTableChange"
              >
                <template #bodyCell="{ column, record }">
                  <template v-if="column.key === 'has_value'">
                    <a-tag :color="record.has_value ? 'green' : 'default'">
                      {{ record.has_value ? t('serverDetail.configured') : t('serverDetail.emptyValue') }}
                    </a-tag>
                  </template>
                  <template v-else-if="column.key === 'actions'">
                    <a-space :size="4">
                      <a-button size="small" @click="openCredential(record)">{{ t('common.edit') }}</a-button>
                      <a-popconfirm :title="t('serverDetail.confirmDeleteCred')" @confirm="removeCredential(record)">
                        <a-button size="small" danger>{{ t('common.delete') }}</a-button>
                      </a-popconfirm>
                    </a-space>
                  </template>
                </template>
              </a-table>
            </a-card>
          </a-col>
        </a-row>
      </a-tab-pane>
    </a-tabs>

    <!-- 凭证创建/编辑弹窗 -->
    <a-modal v-model:open="credVisible" :title="credEditId ? t('serverDetail.editCredential') : t('serverDetail.newCredential')" :ok-text="t('serverDetail.saveCredential')" :cancel-text="t('common.cancel')" :confirm-loading="credSaving" @ok="saveCredential">
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item :label="t('serverDetail.credName')" :required="true">
          <a-input v-model:value="credForm.name" :placeholder="t('serverDetail.credNamePlaceholder')" />
        </a-form-item>
        <a-form-item :label="t('serverDetail.credKind')">
          <a-select v-model:value="credForm.kind">
            <a-select-option value="api_key">{{ t('serverDetail.kindApiKey') }}</a-select-option>
            <a-select-option value="static_token">{{ t('serverDetail.kindStaticToken') }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item v-if="credForm.kind === 'api_key'" :label="t('serverDetail.credHeader')">
          <a-input v-model:value="credForm.header" :placeholder="t('serverDetail.credHeaderPlaceholder')" />
        </a-form-item>
        <a-form-item :label="t('serverDetail.credValue')">
          <a-input-password v-model:value="credForm.value" :placeholder="credEditId ? t('serverDetail.credValueEditPlaceholder') : t('serverDetail.credValuePlaceholder')" />
        </a-form-item>
      </a-form>
    </a-modal>
  </a-spin>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { PlusOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons-vue'
import {
  createCredential,
  deleteCredential,
  getLogs,
  getServer,
  listServerCredentials,
  listServerTools,
  listServerToolsAll,
  testServer,
  toggleServer,
  updateCredential,
  updateServer,
} from '@/api'
import type { Credential, MCPServer, Tool, TrafficSample } from '@/types'
import ToolInvoker from '@/components/ToolInvoker.vue'

const { t } = useI18n()
const route = useRoute()
const id = computed(() => String(route.params.id))
const server = ref<MCPServer>()
// tools = 该 Server 全量工具集（供 Testing 页 ToolInvoker 与 rediscover 后回填）。
const tools = ref<Tool[]>([])
const loading = ref(false)
const testing = ref(false)
const tab = ref('overview')

// 列表错误状态（除 success 外，后端落 status=errs.Code 字符串）。
const errorStatuses = [
  'protocol_error',
  'authentication_error',
  'authorization_error',
  'rate_limit_error',
  'route_error',
  'upstream_error',
  'timeout_error',
  'invalid_argument',
  'not_found',
  'internal_error',
]

const toolColumns = computed<any[]>(() => [
  { title: t('tools.gatewayName'), key: 'gateway_name', dataIndex: 'gateway_name' },
  { title: t('tools.original'), key: 'original_name', dataIndex: 'original_name', width: 150 },
  { title: t('tools.description'), key: 'description', dataIndex: 'description', ellipsis: true },
])

// MVP 单实例承载：Instances 页展示该 Server 的"唯一实例"（endpoint）。
const instanceRows = computed(() =>
  server.value ? [{ endpoint: server.value.endpoint, transport: server.value.transport, health_status: server.value.health_status }] : [],
)
const instanceColumns = computed<any[]>(() => [
  { title: t('serverDetail.endpoint'), key: 'endpoint', dataIndex: 'endpoint' },
  { title: t('serverDetail.transport'), key: 'transport', dataIndex: 'transport', width: 120 },
  { title: t('servers.health'), key: 'health_status', dataIndex: 'health_status', width: 120 },
])
const instanceCount = computed(() => (server.value ? 1 : 0))

const logColumns = computed<any[]>(() => [
  { title: t('traffic.tool'), key: 'tool', dataIndex: 'tool' },
  { title: t('traffic.status'), key: 'status', dataIndex: 'status', width: 130 },
  { title: t('traffic.latencyMs'), key: 'latency_ms', dataIndex: 'latency_ms', width: 90 },
  { title: t('traffic.client'), key: 'client', dataIndex: 'client', width: 110 },
  { title: t('traffic.error'), key: 'error', dataIndex: 'error', ellipsis: true },
  { title: t('traffic.time'), key: 'timestamp', dataIndex: 'timestamp', width: 170 },
])

const credColumns = computed<any[]>(() => [
  { title: t('common.name'), key: 'name', dataIndex: 'name' },
  { title: t('serverDetail.credKind'), key: 'kind', dataIndex: 'kind', width: 130 },
  { title: t('serverDetail.credHeader'), key: 'header', dataIndex: 'header', width: 150 },
  { title: t('serverDetail.valueColumn'), key: 'has_value', width: 90 },
  { title: t('common.actions'), key: 'actions', width: 150 },
])

// ---- Tools tab（分页）----
const toolRows = ref<Tool[]>([])
const toolsLoading = ref(false)
const toolFilters = reactive({ q: '' })
const toolPaging = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => t('filter.total', { total }),
})

async function loadToolPage() {
  toolsLoading.value = true
  try {
    const res = await listServerTools(id.value, {
      page: toolPaging.current,
      page_size: toolPaging.pageSize,
      q: toolFilters.q.trim() || undefined,
    })
    toolRows.value = res.items
    toolPaging.total = res.total
    if (res.items.length === 0 && toolPaging.current > 1 && res.total > 0) {
      toolPaging.current -= 1
      await loadToolPage()
      return
    }
  } catch (e) {
    message.error(String(e))
  } finally {
    toolsLoading.value = false
  }
}

function onToolFilter() {
  toolPaging.current = 1
  void loadToolPage()
}

function onToolTableChange(p: { current?: number; pageSize?: number }) {
  if (p.current) toolPaging.current = p.current
  if (p.pageSize && p.pageSize !== toolPaging.pageSize) {
    toolPaging.pageSize = p.pageSize
    toolPaging.current = 1
  }
  void loadToolPage()
}

// ---- Logs tab（分页，server 取自路径）----
const logs = ref<TrafficSample[]>([])
const logsLoading = ref(false)
const logFilters = reactive({ q: '', status: '' })
const logPaging = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => t('filter.total', { total }),
})

async function loadLogsPage() {
  logsLoading.value = true
  try {
    const res = await getLogs({
      server_id: id.value,
      page: logPaging.current,
      page_size: logPaging.pageSize,
      q: logFilters.q.trim() || undefined,
      status: logFilters.status || undefined,
    })
    logs.value = res.items
    logPaging.total = res.total
    if (res.items.length === 0 && logPaging.current > 1 && res.total > 0) {
      logPaging.current -= 1
      await loadLogsPage()
      return
    }
  } catch (e) {
    message.error(String(e))
  } finally {
    logsLoading.value = false
  }
}

function onLogFilter() {
  logPaging.current = 1
  void loadLogsPage()
}

function onLogTableChange(p: { current?: number; pageSize?: number }) {
  if (p.current) logPaging.current = p.current
  if (p.pageSize && p.pageSize !== logPaging.pageSize) {
    logPaging.pageSize = p.pageSize
    logPaging.current = 1
  }
  void loadLogsPage()
}

// ---- Configuration：Server 可编辑字段 + 凭证管理（分页）----
const editForm = reactive<{ description: string; endpoint: string; transport: MCPServer['transport'] }>({
  description: '',
  endpoint: '',
  transport: 'https',
})
const savingServer = ref(false)

const credentials = ref<Credential[]>([])
const credsLoading = ref(false)
const credFilters = reactive({ q: '', kind: '', hasValue: '' })
const credPaging = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => t('filter.total', { total }),
})

async function loadCredentialsPage() {
  credsLoading.value = true
  try {
    const res = await listServerCredentials(id.value, {
      page: credPaging.current,
      page_size: credPaging.pageSize,
      q: credFilters.q.trim() || undefined,
      kind: (credFilters.kind as Credential['kind'] | '') || undefined,
      has_value: credFilters.hasValue === 'true' ? true : credFilters.hasValue === 'false' ? false : undefined,
    })
    credentials.value = res.items
    credPaging.total = res.total
    if (res.items.length === 0 && credPaging.current > 1 && res.total > 0) {
      credPaging.current -= 1
      await loadCredentialsPage()
      return
    }
  } catch (e) {
    message.error(String(e))
  } finally {
    credsLoading.value = false
  }
}

function onCredFilter() {
  credPaging.current = 1
  void loadCredentialsPage()
}

function onCredTableChange(p: { current?: number; pageSize?: number }) {
  if (p.current) credPaging.current = p.current
  if (p.pageSize && p.pageSize !== credPaging.pageSize) {
    credPaging.pageSize = p.pageSize
    credPaging.current = 1
  }
  void loadCredentialsPage()
}

onMounted(load)

async function load() {
  loading.value = true
  try {
    const [srv, fullTools] = await Promise.all([getServer(id.value), listServerToolsAll(id.value)])
    server.value = srv
    tools.value = fullTools
    syncEditForm(srv)
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
  await Promise.allSettled([loadToolPage(), loadLogsPage(), loadCredentialsPage()])
}

async function test() {
  try {
    await testServer(id.value)
    message.success(t('serverDetail.testOk'))
    await load()
  } catch (e) {
    message.error(`${t('serverDetail.connectFail')}: ${e}`)
  }
}

// Testing 页：重新发现并刷新工具（全量 + 分页表同步刷新）。
async function testAndReloadTools() {
  testing.value = true
  try {
    await testServer(id.value)
    tools.value = await listServerToolsAll(id.value)
    await loadToolPage()
    message.success(t('serverDetail.testOk'))
  } catch (e) {
    message.error(`${t('serverDetail.connectFail')}: ${e}`)
  } finally {
    testing.value = false
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

function syncEditForm(srv: MCPServer) {
  editForm.description = srv.description ?? ''
  editForm.endpoint = srv.endpoint
  editForm.transport = srv.transport
}

async function saveServerEdit() {
  if (!editForm.endpoint.trim()) {
    message.warning(t('servers.fillRequired'))
    return
  }
  savingServer.value = true
  try {
    const updated = await updateServer(id.value, {
      description: editForm.description,
      endpoint: editForm.endpoint,
      transport: editForm.transport,
    })
    server.value = updated
    syncEditForm(updated)
    message.success(t('servers.updatedOk'))
  } catch (e) {
    message.error(String(e))
  } finally {
    savingServer.value = false
  }
}

// ---- Credential 表单（创建/编辑共用；编辑时空 value 表示不改值）----
const credVisible = ref(false)
const credSaving = ref(false)
const credEditId = ref('')
const credForm = reactive<{ name: string; kind: 'api_key' | 'static_token'; header: string; value: string }>({
  name: '',
  kind: 'api_key',
  header: '',
  value: '',
})

function openCredential(record?: Credential) {
  if (record) {
    credEditId.value = record.id
    Object.assign(credForm, {
      name: record.name,
      kind: record.kind,
      header: record.header ?? '',
      value: '',
    })
  } else {
    credEditId.value = ''
    Object.assign(credForm, { name: '', kind: 'api_key', header: '', value: '' })
  }
  credVisible.value = true
}

async function saveCredential() {
  if (!credForm.name) {
    message.warning(t('serverDetail.credentialNameRequired'))
    return
  }
  credSaving.value = true
  try {
    if (credEditId.value) {
      await updateCredential(id.value, credEditId.value, { ...credForm })
    } else {
      await createCredential(id.value, { ...credForm })
      credPaging.current = 1
    }
    message.success(t('serverDetail.credentialSaved'))
    credVisible.value = false
    await loadCredentialsPage()
  } catch (e) {
    message.error(String(e))
  } finally {
    credSaving.value = false
  }
}

async function removeCredential(record: Credential) {
  try {
    await deleteCredential(id.value, record.id)
    message.success(t('serverDetail.credentialDeleted'))
    await loadCredentialsPage()
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
.toolbar {
  margin-bottom: 12px;
}
.filters {
  margin-bottom: 12px;
}
</style>
