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
          <template v-else-if="column.key === 'requests'">{{ metricByID[record.id]?.totals ?? '-' }}</template>
          <template v-else-if="column.key === 'p95'">{{ metricByID[record.id] ? `${metricByID[record.id].p95.toFixed(0)}ms` : '-' }}</template>
          <template v-else-if="column.key === 'actions'">
            <a-space :size="4">
              <a-button size="small" @click="openEdit(record)">{{ t('servers.edit') }}</a-button>
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

    <a-modal v-model:open="dialogVisible" :title="editing ? t('servers.editTitle') : t('servers.dialogTitle')" :confirm-loading="submitting" @ok="submit" :ok-text="editing ? t('servers.saveEdit') : t('servers.register')" :cancel-text="t('common.cancel')">
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item :label="t('common.name')" :required="!editing">
          <a-input v-model:value="form.name" :disabled="editing" :placeholder="editing ? t('servers.nameReadonlyHint') : 'University MCP'" />
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

        <template v-if="!editing">
          <a-divider orientation="left" class="auth-divider">{{ t('servers.auth') }}</a-divider>
          <div v-if="reqHeaders.length" class="auth-rows">
            <div v-for="(row, i) in reqHeaders" :key="i" class="auth-row">
              <a-input
                v-model:value="row.header"
                :disabled="row.bearer"
                :placeholder="row.bearer ? 'Authorization' : t('servers.authHeaderPh')"
                class="auth-header"
              />
              <a-checkbox v-model:checked="row.bearer">{{ t('servers.authBearer') }}</a-checkbox>
              <a-input-password v-model:value="row.value" :placeholder="t('servers.authValuePh')" class="auth-value" />
              <a-button type="text" size="small" @click="reqHeaders.splice(i, 1)">
                <template #icon><DeleteOutlined /></template>
              </a-button>
            </div>
          </div>
          <div v-else class="auth-empty">{{ t('servers.authEmpty') }}</div>
          <a-button type="dashed" block @click="addAuthRow">
            <template #icon><PlusOutlined /></template>{{ t('servers.authAdd') }}
          </a-button>
        </template>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { PlusOutlined, DeleteOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { createCredential, createServer, deleteServer, getServerMetrics, listServers, listServerTools, testServer, toggleServer, updateServer } from '@/api'
import type { MCPServer, MetricSnapshot, Transport } from '@/types'

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
// 注册时可配置任意多条"上游请求头"（可选）：每条 = 请求头名 + 值，
// 勾选 Bearer 时该条固定注入 Authorization: Bearer <值>（走 static_token）。
// 落库为 Credential，值 AES 加密；header 可留空代表无附加头。
interface AuthRow {
  header: string
  bearer: boolean
  value: string
}
const reqHeaders = ref<AuthRow[]>([])
// 编辑态：true 时同一弹窗承担"编辑 Server 可编辑字段"（name 只读、不配凭证）。
const editing = ref(false)
const editingId = ref('')
// 按 Server 聚合的指标（key = server:<id>），供 Requests / P95 列。
const serverMetrics = ref<MetricSnapshot[]>([])
const metricByID = computed<Record<string, MetricSnapshot>>(() => {
  const m: Record<string, MetricSnapshot> = {}
  for (const s of serverMetrics.value) {
    const id = s.key.startsWith('server:') ? s.key.slice('server:'.length) : s.key
    m[id] = s
  }
  return m
})

const columns = computed<any[]>(() => [
  { title: t('common.name'), key: 'name', dataIndex: 'name' },
  { title: t('servers.endpoint'), key: 'endpoint', dataIndex: 'endpoint', ellipsis: true },
  { title: t('servers.transport'), key: 'transport', dataIndex: 'transport', width: 110 },
  { title: t('servers.tools'), key: 'tools', width: 70 },
  { title: t('servers.requests'), key: 'requests', width: 90 },
  { title: t('servers.p95'), key: 'p95', width: 80 },
  { title: t('servers.health'), key: 'health', dataIndex: 'health_status', width: 100 },
  { title: t('common.status'), key: 'enabled', dataIndex: 'enabled', width: 100 },
  { title: t('common.actions'), key: 'actions', width: 300 },
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
    try {
      serverMetrics.value = await getServerMetrics()
    } catch {
      serverMetrics.value = []
    }
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = false
  editingId.value = ''
  form.name = ''
  form.endpoint = ''
  form.transport = 'https'
  form.description = ''
  reqHeaders.value = []
  dialogVisible.value = true
}

// 编辑：name 不可改（后端会拒绝改名），仅携带可编辑字段。
function openEdit(row: MCPServer) {
  editing.value = true
  editingId.value = row.id
  form.name = row.name
  form.endpoint = row.endpoint
  form.transport = row.transport
  form.description = row.description ?? ''
  reqHeaders.value = []
  dialogVisible.value = true
}

function addAuthRow() {
  reqHeaders.value.push({ header: '', bearer: false, value: '' })
}

// 校验并生成要落库的凭证列表：密钥值未填的行视为无效并忽略；
// 自定义 Header 行（非 Bearer）必须填写请求头名，否则拦截提醒。
function buildAuthCredentials() {
  const rows: { kind: 'api_key' | 'static_token'; header: string; value: string }[] = []
  for (const row of reqHeaders.value) {
    if (!row.value) continue // 无密钥值 → 忽略该行
    if (!row.bearer && !row.header) return null // 有值但缺 header
    rows.push({
      kind: row.bearer ? 'static_token' : 'api_key',
      header: row.bearer ? '' : row.header,
      value: row.value,
    })
  }
  return rows
}

async function submit() {
  if (!form.name || !form.endpoint) {
    message.warning(t('servers.fillRequired'))
    return
  }
  submitting.value = true
  // 编辑态：name 不可改，仅提交可编辑字段。
  if (editing.value) {
    try {
      await updateServer(editingId.value, {
        description: form.description,
        endpoint: form.endpoint,
        transport: form.transport,
      })
      message.success(t('servers.updatedOk'))
      dialogVisible.value = false
      await load()
    } catch (e) {
      message.error(String(e))
    } finally {
      submitting.value = false
    }
    return
  }
  const authRows = buildAuthCredentials()
  if (authRows === null) {
    message.warning(t('servers.authHeaderRequired'))
    return
  }
  submitting.value = true
  try {
    const server = await createServer({ ...form })
    // 注册时若有请求头配置，随即逐条落 Credential（值加密存储）。
    const failures: string[] = []
    for (const [i, row] of authRows.entries()) {
      try {
        await createCredential(server.id, {
          name: `${server.name} auth#${i + 1}`,
          kind: row.kind,
          header: row.header,
          value: row.value,
        })
      } catch (e) {
        failures.push(String(e))
      }
    }
    if (failures.length) {
      message.warning(`${t('servers.registeredOk')}，${t('servers.authFail')}: ${failures[0]}`)
    } else if (authRows.length) {
      message.success(t('servers.authApplied'))
    } else {
      message.success(t('servers.registeredOk'))
    }
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
.auth-divider {
  margin-top: 8px;
}
.auth-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 8px;
}
.auth-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.auth-header {
  flex: 0 0 200px;
}
.auth-value {
  flex: 1;
}
.auth-empty {
  color: #999;
  font-size: 12px;
  padding: 2px 0 8px;
}
</style>