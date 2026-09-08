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
          <a-descriptions-item :label="t('serverDetail.endpoint')">
            <span class="mono">{{ primaryEndpoint(server) }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('serverDetail.transport')">{{ primaryTransport(server) }}</a-descriptions-item>
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
        <div class="toolbar">
          <a-space wrap :size="8">
            <a-input v-model:value="toolFilters.q" allow-clear :placeholder="t('filter.keyword')" style="width: 240px" @press-enter="onToolFilter">
              <template #prefix><SearchOutlined /></template>
            </a-input>
            <a-button size="small" :loading="rediscovering" @click="openRediscoverPreview">
              <template #icon><ReloadOutlined /></template>{{ t('serverDetail.rediscover') }}
            </a-button>
          </a-space>
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

      <!-- Instances：真实多实例管理（endpoint/transport/健康/启停/删除） -->
      <a-tab-pane :key="'instances'" :tab="t('serverDetail.instances')">
        <div class="toolbar">
          <a-space>
            <a-button type="primary" size="small" @click="openInstance()">
              <template #icon><PlusOutlined /></template>{{ t('serverDetail.addInstance') }}
            </a-button>
            <a-button size="small" @click="loadInstances">
              <template #icon><ReloadOutlined /></template>{{ t('common.refresh') }}
            </a-button>
          </a-space>
        </div>
        <a-table
          :data-source="instanceRowsWithMetrics"
          :columns="instanceColumns"
          :loading="instancesLoading"
          :pagination="false"
          size="small"
          :row-key="(r: any) => r.id"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'health_status'">
              <a-tag :color="healthColor(record.health_status)" :bordered="false">{{ record.health_status }}</a-tag>
            </template>
            <template v-else-if="column.key === 'enabled'">
              <a-tag :color="record.enabled ? 'green' : 'default'">
                {{ record.enabled ? t('servers.enabled') : t('servers.disabled') }}
              </a-tag>
            </template>
            <template v-else-if="column.key === 'm_requests' || column.key === 'm_errors'">
              <span>{{ record[column.key] === '-' ? '-' : record[column.key] }}</span>
            </template>
            <template v-else-if="column.key === 'm_p95'">
              <span class="mono">{{ record.m_p95 === '-' ? '-' : `${record.m_p95}ms` }}</span>
            </template>
            <template v-else-if="column.key === 'actions'">
              <a-space :size="4">
                <a-button size="small" @click="openInstance(record)">{{ t('common.edit') }}</a-button>
                <a-button size="small" :loading="testingInst === record.id" @click="testInstance(record)">{{ t('servers.test') }}</a-button>
                <a-button size="small" :danger="record.enabled" @click="toggleInstance(record)">
                  {{ record.enabled ? t('servers.disable') : t('servers.enable') }}
                </a-button>
                <a-button size="small" danger @click="requestDeleteInstance(record)">{{ t('common.delete') }}</a-button>
              </a-space>
            </template>
          </template>
        </a-table>
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
                <a-form-item :wrapper-col="{ offset: 6, span: 18 }">
                  <a-button type="primary" :loading="savingServer" @click="saveServerEdit">{{ t('serverDetail.saveServer') }}</a-button>
                  <span class="endpoint-edit-hint">{{ t('serverDetail.endpointEditGoesToInstances') }}</span>
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
                      <a-button size="small" danger @click="requestDeleteCredential(record)">{{ t('common.delete') }}</a-button>
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
    <!-- 实例创建/编辑弹窗（endpoint/transport 编辑后健康复位待探活） -->
    <a-modal
      v-model:open="instVisible"
      :title="instEditId ? t('serverDetail.editInstance') : t('serverDetail.addInstance')"
      :ok-text="t('serverDetail.saveInstance')"
      :cancel-text="t('common.cancel')"
      :confirm-loading="instSaving"
      @ok="saveInstance"
    >
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item :label="isInstStdio ? t('serverDetail.instanceCommand') : t('serverDetail.instanceEndpoint')" :required="true">
          <a-input v-model:value="instForm.endpoint" :placeholder="isInstStdio ? t('servers.commandPlaceholder') : t('servers.endpointPlaceholder')" />
        </a-form-item>
        <a-form-item :label="t('serverDetail.instanceTransport')">
          <a-select v-model:value="instForm.transport">
            <a-select-option value="https">{{ t('servers.transportStreamable') }}</a-select-option>
            <a-select-option value="sse">{{ t('servers.transportSSE') }}</a-select-option>
            <a-select-option value="stdio">{{ t('servers.transportStdio') }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item v-if="isInstStdio" :label="t('servers.args')">
          <a-input v-model:value="instForm.args" :placeholder="t('servers.argsPlaceholder')" />
          <span class="inst-args-hint">{{ t('servers.stdioHint') }}</span>
        </a-form-item>
      </a-form>
    </a-modal>
    <!-- 重新发现 Tools：只读预演 → 变更对比 → 人工确认后再真正应用 -->
    <a-modal
      v-model:open="planVisible"
      :title="t('serverDetail.rediscoverPreviewTitle')"
      :width="760"
      :ok-text="t('serverDetail.rediscoverApply')"
      :cancel-text="t('common.cancel')"
      :ok-button-props="{ disabled: !((plan?.changes?.length ?? 0) || (plan?.conflicts?.length ?? 0)) }"
      :confirm-loading="applying"
      @ok="applyRediscover"
    >
      <a-alert
        v-if="plan && (plan.conflicts?.length ?? 0) > 0"
        type="error"
        show-icon
        class="mb"
        :message="t('serverDetail.rediscoverConflictTitle')"
      >
        <template #description>
          <div>{{ t('serverDetail.rediscoverConflictHint') }}</div>
          <ul class="plan-conflicts">
            <li v-for="conflict in plan.conflicts" :key="conflict.gateway_name">
              {{ t('serverDetail.rediscoverConflictItem', { tool: conflict.gateway_name, server: conflict.owner_server_name }) }}
            </li>
          </ul>
        </template>
      </a-alert>
      <a-alert
        v-if="plan && !(plan.changes?.length ?? 0)"
        type="info"
        show-icon
        class="mb"
        :message="t('serverDetail.rediscoverNoChange')"
      />
      <template v-if="plan && (plan.changes?.length ?? 0) > 0">
        <div class="plan-summary">
          <a-tag color="green">{{ t('serverDetail.rediscoverAdd') }} {{ plan.added }}</a-tag>
          <a-tag color="orange">{{ t('serverDetail.rediscoverUpdate') }} {{ plan.updated }}</a-tag>
        </div>
        <a-table
          size="small"
          :data-source="plan.changes"
          :columns="planColumns"
          :pagination="false"
          :row-key="(r: RediscoverChange) => `${r.kind}:${r.original_name}`"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'kind'">
              <a-tag :color="record.kind === 'add' ? 'green' : 'orange'" :bordered="false">
                {{ record.kind === 'add' ? t('serverDetail.rediscoverAdd') : t('serverDetail.rediscoverUpdate') }}
              </a-tag>
            </template>
            <template v-else-if="column.key === 'tool'">
              <div class="mono">{{ record.original_name }}</div>
              <div class="muted plan-gw">{{ record.gateway_name }}</div>
            </template>
            <template v-else-if="column.key === 'change'">
              <span v-if="record.kind === 'add'">{{ t('serverDetail.rediscoverAddDetail') }}</span>
              <span v-else>{{ changeText(record) }}</span>
            </template>
          </template>
        </a-table>
        <div class="plan-hint muted">{{ t('serverDetail.rediscoverConfirmHint') }}</div>
      </template>
    </a-modal>
    <DeleteConfirmModal
      v-model:open="deleteInstanceVisible"
      :title="t('serverDetail.deleteInstanceTitle')"
      :description="t('serverDetail.deleteInstanceWarning')"
      :target="deleteInstanceTarget?.endpoint"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :loading="deletingInstance"
      @confirm="confirmDeleteInstance"
    />
    <DeleteConfirmModal
      v-model:open="deleteCredentialVisible"
      :title="t('serverDetail.deleteCredentialTitle')"
      :description="t('serverDetail.deleteCredentialWarning')"
      :target="deleteCredentialTarget?.name"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :loading="deletingCredential"
      @confirm="confirmDeleteCredential"
    />
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
  createServerInstance,
  deleteCredential,
  deleteServerInstance,
  getInstanceMetrics,
  getLogs,
  getServer,
  listServerCredentials,
  listServerInstances,
  listServerTools,
  previewRediscover,
  primaryEndpoint,
  primaryTransport,
  testServer,
  testServerInstance,
  toggleServer,
  toggleServerInstance,
  updateCredential,
  updateServer,
  updateServerInstance,
} from '@/api'
import DeleteConfirmModal from '@/components/DeleteConfirmModal.vue'
import type { Credential, MCPServer, MetricSnapshot, RediscoverChange, RediscoverPlan, ServerInstance, Tool, TrafficSample, Transport } from '@/types'

const { t } = useI18n()
const route = useRoute()
const id = computed(() => String(route.params.id))
const server = ref<MCPServer>()
const loading = ref(false)
const rediscovering = ref(false)
const planVisible = ref(false)
const applying = ref(false)
const plan = ref<RediscoverPlan | null>(null)
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

// ---- Instances tab：真实多实例管理 ----
const instanceRows = ref<ServerInstance[]>([])
const instancesLoading = ref(false)
const testingInst = ref('')
// 实例维指标（键 = instance:<sid>:<iid> → 剥离前缀，按实例 id 取请求量/延迟）。
const instanceMetrics = ref<Record<string, MetricSnapshot>>({})
const instanceColumns = computed<any[]>(() => [
  { title: t('serverDetail.endpoint'), key: 'endpoint', dataIndex: 'endpoint_display', ellipsis: true },
  { title: t('serverDetail.transport'), key: 'transport', dataIndex: 'transport', width: 100 },
  { title: t('servers.health'), key: 'health_status', dataIndex: 'health_status', width: 105 },
  { title: t('common.status'), key: 'enabled', dataIndex: 'enabled', width: 85 },
  { title: t('traffic.requests'), key: 'm_requests', width: 90 },
  { title: t('observability.p95'), key: 'm_p95', width: 90 },
  { title: t('traffic.errors'), key: 'm_errors', width: 90 },
  { title: t('common.updated'), key: 'updated_at', dataIndex: 'updated_at', width: 165 },
  { title: t('common.actions'), key: 'actions', width: 265 },
])
// 实例行 + 该实例的实时指标（为空显示 '-'；bodyCell 里格式化）。
// 端点展示：stdio 实例附启动参数（command arg1 arg2），便于辨识子进程配置。
const instanceRowsWithMetrics = computed<any[]>(() =>
  instanceRows.value.map((r) => {
    const m = instanceMetrics.value[r.id]
    const endpoint =
      r.transport === 'stdio' && r.args?.length ? `${r.endpoint} ${r.args.join(' ')}` : r.endpoint
    return { ...r, endpoint_display: endpoint, m_requests: m?.totals ?? '-', m_p95: m?.p95 ?? '-', m_errors: m?.errors ?? '-' }
  }),
)
const instanceCount = computed(() => instanceRows.value.length)

const instVisible = ref(false)
const instSaving = ref(false)
const deleteInstanceVisible = ref(false)
const deleteInstanceTarget = ref<ServerInstance | null>(null)
const deletingInstance = ref(false)
const instEditId = ref('')
const instForm = reactive<{ endpoint: string; transport: Transport; args: string }>({ endpoint: '', transport: 'https', args: '' })
// stdio 传输：endpoint 承载可执行命令并展示启动参数输入框。
const isInstStdio = computed(() => instForm.transport === 'stdio')

// 解析 stdio 启动参数（JSON 数组文本）；非法或非字符串数组时返回 null 供拦截。
function parseStdioArgs(text: string): string[] | null {
  const trimmed = text.trim()
  if (!trimmed) return []
  try {
    const parsed = JSON.parse(trimmed)
    if (!Array.isArray(parsed) || parsed.some((a) => typeof a !== 'string')) return null
    return parsed as string[]
  } catch {
    return null
  }
}

async function loadInstances() {
  instancesLoading.value = true
  try {
    instanceRows.value = await listServerInstances(id.value)
    await loadInstanceMetrics()
  } catch (e) {
    message.error(String(e))
  } finally {
    instancesLoading.value = false
  }
}

// loadInstanceMetrics 拉取该 Server 的实例维指标，键为 instance:<sid>:<iid>，
// 剥离前缀后按实例 id 落表（仅展示；拉取失败不清空已有，避免闪烁）。
async function loadInstanceMetrics() {
  try {
    const rows = await getInstanceMetrics(id.value)
    const byID: Record<string, MetricSnapshot> = {}
    const prefix = `instance:${id.value}:`
    for (const row of rows) {
      if (row.key.startsWith(prefix)) byID[row.key.slice(prefix.length)] = row
    }
    instanceMetrics.value = byID
  } catch {
    /* 忽略：指标拉取失败不影响实例启停等核心操作 */
  }
}

function openInstance(record?: ServerInstance) {
  if (record) {
    instEditId.value = record.id
    Object.assign(instForm, {
      endpoint: record.endpoint,
      transport: record.transport,
      args: record.args?.length ? JSON.stringify(record.args) : '',
    })
  } else {
    instEditId.value = ''
    Object.assign(instForm, { endpoint: '', transport: 'https', args: '' })
  }
  instVisible.value = true
}

async function saveInstance() {
  if (!instForm.endpoint.trim()) {
    message.warning(t('servers.fillRequired'))
    return
  }
  const args = isInstStdio.value ? parseStdioArgs(instForm.args) : undefined
  if (args === null) {
    message.warning(t('servers.argsInvalid'))
    return
  }
  instSaving.value = true
  try {
    const payload = { endpoint: instForm.endpoint, transport: instForm.transport, args }
    if (instEditId.value) {
      await updateServerInstance(id.value, instEditId.value, payload)
    } else {
      await createServerInstance(id.value, payload)
    }
    message.success(t('serverDetail.instanceSaved'))
    instVisible.value = false
    await loadInstances()
  } catch (e) {
    message.error(String(e))
  } finally {
    instSaving.value = false
  }
}

async function toggleInstance(record: ServerInstance) {
  try {
    await toggleServerInstance(id.value, record.id, !record.enabled)
    await loadInstances()
  } catch (e) {
    message.error(String(e))
  }
}

async function testInstance(record: ServerInstance) {
  testingInst.value = record.id
  try {
    const updated = await testServerInstance(id.value, record.id)
    message.success(`${t('serverDetail.testOk')} · ${updated.health_status}`)
  } catch (e) {
    message.error(`${t('serverDetail.connectFail')}: ${e}`)
  } finally {
    // 无论成败都刷新实例列表：失败时后端已把该实例标 unhealthy，行状态需与落库一致。
    testingInst.value = ''
    await loadInstances()
  }
}

function requestDeleteInstance(record: ServerInstance) {
  deleteInstanceTarget.value = record
  deleteInstanceVisible.value = true
}

async function confirmDeleteInstance() {
  const record = deleteInstanceTarget.value
  if (!record) return
  deletingInstance.value = true
  try {
    await deleteServerInstance(id.value, record.id)
    message.success(t('serverDetail.instanceDeleted'))
    deleteInstanceVisible.value = false
    deleteInstanceTarget.value = null
    await loadInstances()
  } catch (e) {
    message.error(String(e))
  } finally {
    deletingInstance.value = false
  }
}

const logColumns = computed<any[]>(() => [
  { title: t('traffic.tool'), key: 'tool', dataIndex: 'tool' },
  { title: t('traffic.status'), key: 'status', dataIndex: 'status', width: 130 },
  { title: t('traffic.latencyMs'), key: 'latency_ms', dataIndex: 'latency_ms', width: 90 },
  { title: t('traffic.client'), key: 'client', dataIndex: 'client', width: 110 },
  { title: t('traffic.clientIp'), key: 'client_ip', dataIndex: 'client_ip', width: 130 },
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
// status 下拉初始为 undefined：antd Select 仅在值为空(null/undefined)时展示占位文案。
const logFilters = reactive<{ q: string; status?: string }>({ q: '' })
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

// ---- Configuration：Server 逻辑字段（description；端点编辑在 Instances 页）----
const editForm = reactive<{ description: string }>({ description: '' })
const savingServer = ref(false)

const credentials = ref<Credential[]>([])
const credsLoading = ref(false)
// kind/hasValue 下拉初始为 undefined：antd Select 仅在值为空(null/undefined)时展示占位文案。
const credFilters = reactive<{ q: string; kind?: string; hasValue?: string }>({ q: '' })
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
    const srv = await getServer(id.value)
    server.value = srv
    syncEditForm(srv)
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
  await Promise.allSettled([loadToolPage(), loadLogsPage(), loadCredentialsPage(), loadInstances()])
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

// Tools 列表页「重新发现」：先只读预演拿变更，弹窗对比后由人工确认再应用。
async function openRediscoverPreview() {
  rediscovering.value = true
  plan.value = null
  try {
    plan.value = await previewRediscover(id.value)
    planVisible.value = true
  } catch (e) {
    message.error(`${t('serverDetail.rediscoverPlanFail')}: ${e}`)
  } finally {
    rediscovering.value = false
  }
}

// 人工确认后真正应用（与 POST /servers/:id/test 语义一致：落库并按覆盖保护刷新）。
async function applyRediscover() {
  applying.value = true
  try {
    await testServer(id.value)
    planVisible.value = false
    await loadToolPage()
    server.value = await getServer(id.value)
    message.success(t('serverDetail.rediscovered'))
  } catch (e) {
    message.error(`${t('serverDetail.connectFail')}: ${e}`)
  } finally {
    applying.value = false
  }
}

const planColumns = computed<any[]>(() => [
  { title: t('serverDetail.rediscoverColType'), key: 'kind', width: 76 },
  { title: t('serverDetail.rediscoverColTool'), key: 'tool' },
  { title: t('serverDetail.rediscoverColChange'), key: 'change' },
])

// 把服务端给出的结构化变更压缩成一行可读说明（update 用；add 走固定文案）。
function changeText(c: RediscoverChange): string {
  const parts: string[] = []
  if (c.desc_changed) parts.push(`${t('serverDetail.rediscoverFieldDesc')}${t('serverDetail.rediscoverWillChange')}`)
  if (c.schema_changed) parts.push(`${t('serverDetail.rediscoverFieldSchema')}${t('serverDetail.rediscoverWillChange')}`)
  if (c.source_changed) parts.push(t('serverDetail.rediscoverSourceRefresh'))
  if (!parts.length) parts.push(t('serverDetail.rediscoverNoChangeRow'))
  let text = parts.join(' · ')
  if (c.desc_protected || c.schema_protected || c.name_protected) text += `（${t('serverDetail.rediscoverProtected')}）`
  return text
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
}

async function saveServerEdit() {
  savingServer.value = true
  try {
    const updated = await updateServer(id.value, { description: editForm.description })
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
const deleteCredentialVisible = ref(false)
const deleteCredentialTarget = ref<Credential | null>(null)
const deletingCredential = ref(false)
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

function requestDeleteCredential(record: Credential) {
  deleteCredentialTarget.value = record
  deleteCredentialVisible.value = true
}

async function confirmDeleteCredential() {
  const record = deleteCredentialTarget.value
  if (!record) return
  deletingCredential.value = true
  try {
    await deleteCredential(id.value, record.id)
    message.success(t('serverDetail.credentialDeleted'))
    deleteCredentialVisible.value = false
    deleteCredentialTarget.value = null
    await loadCredentialsPage()
  } catch (e) {
    message.error(String(e))
  } finally {
    deletingCredential.value = false
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
.endpoint-edit-hint {
  margin-left: 8px;
  color: #999;
  font-size: 12px;
}
.inst-args-hint {
  display: block;
  color: #999;
  font-size: 12px;
  line-height: 18px;
  margin-top: 4px;
}
.muted {
  color: var(--mc-ink-3);
}
.plan-summary {
  margin-bottom: 12px;
}
.plan-conflicts {
  margin: 8px 0 0;
  padding-left: 18px;
}
.plan-gw {
  font-size: 12px;
}
.plan-hint {
  margin-top: 12px;
  font-size: 12px;
}
</style>
