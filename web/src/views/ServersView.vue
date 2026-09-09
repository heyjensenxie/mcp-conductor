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

    <div class="filters">
      <a-space wrap :size="8">
        <a-input v-model:value="filters.q" allow-clear :placeholder="t('filter.keyword')" style="width: 220px" @press-enter="onFilterChange">
          <template #prefix><SearchOutlined /></template>
        </a-input>
        <a-select v-model:value="filters.enabled" allow-clear :placeholder="t('common.status')" style="width: 120px" @change="onFilterChange">
          <a-select-option value="true">{{ t('servers.enabled') }}</a-select-option>
          <a-select-option value="false">{{ t('servers.disabled') }}</a-select-option>
        </a-select>
        <a-select v-model:value="filters.health" allow-clear :placeholder="t('servers.health')" style="width: 140px" @change="onFilterChange">
          <a-select-option v-for="v in healthOptions" :key="v" :value="v">{{ v }}</a-select-option>
        </a-select>
      </a-space>
    </div>

    <a-card :bordered="true">
      <a-table
        :data-source="servers"
        :columns="columns"
        :loading="loading"
        :row-key="(r: any) => r.id"
        :pagination="pagination"
        @change="onTableChange"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <router-link :to="`/servers/${record.id}`" class="link">{{ record.name }}</router-link>
          </template>
          <template v-else-if="column.key === 'endpoint'">
            <span class="mono">{{ primaryEndpoint(record) }}</span>
          </template>
          <template v-else-if="column.key === 'transport'">{{ primaryTransport(record) }}</template>
          <template v-else-if="column.key === 'instances'">
            <a-tag :bordered="false" color="blue">{{ record.instances?.length ?? 0 }}</a-tag>
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
              <a-button size="small" danger @click="requestDelete(record)">{{ t('servers.delete') }}</a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <!-- 新增/编辑统一表单：逻辑 Server（name/description）+ 主实例（endpoint/transport/args）+ 请求头 -->
    <a-modal
      v-model:open="dialogVisible"
      :title="editing ? t('servers.editTitle') : t('servers.dialogTitle')"
      :confirm-loading="submitting"
      :mask-closable="false"
    >
      <template #footer>
        <a-button @click="dialogVisible = false">{{ t('common.cancel') }}</a-button>
        <a-button :loading="testing" @click="testDraft">
          <template #icon><ApiOutlined /></template>{{ t('servers.testConnection') }}
        </a-button>
        <a-button type="primary" :loading="submitting" @click="submit">
          {{ editing ? t('servers.saveEdit') : t('servers.register') }}
        </a-button>
      </template>
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item :label="t('common.name')" :required="true">
          <a-input v-model:value="form.name" :placeholder="t('servers.namePlaceholder')" />
          <span v-if="editing" class="field-hint">{{ t('servers.nameDisplayHint') }}</span>
        </a-form-item>
        <a-form-item :label="t('common.description')">
          <a-textarea v-model:value="form.description" :rows="2" />
        </a-form-item>
        <a-form-item :label="isStdio ? t('servers.command') : t('servers.endpoint')" :required="true">
          <a-input v-model:value="form.endpoint" :placeholder="isStdio ? t('servers.commandPlaceholder') : t('servers.endpointPlaceholder')" />
        </a-form-item>
        <a-form-item :label="t('servers.transport')">
          <a-select v-model:value="form.transport">
            <a-select-option value="https">{{ t('servers.transportStreamable') }}</a-select-option>
            <a-select-option value="sse">{{ t('servers.transportSSE') }}</a-select-option>
            <a-select-option value="stdio">{{ t('servers.transportStdio') }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item v-if="isStdio" :label="t('servers.args')">
          <a-input v-model:value="form.args" :placeholder="t('servers.argsPlaceholder')" />
          <span class="field-hint">{{ t('servers.stdioHint') }}</span>
        </a-form-item>
        <a-form-item v-if="editing" class="edit-hint">
          <span>{{ t('servers.endpointsInDetailHint') }}</span>
        </a-form-item>

        <a-divider orientation="left" class="auth-divider">{{ t('servers.auth') }}</a-divider>
        <div v-if="authLoading" class="auth-empty">{{ t('servers.authLoading') }}</div>
        <template v-else>
          <div v-if="reqHeaders.length" class="auth-rows">
            <div v-for="(row, i) in reqHeaders" :key="i" class="auth-row">
              <a-input
                v-model:value="row.header"
                :disabled="row.bearer"
                :placeholder="row.bearer ? 'Authorization' : t('servers.authHeaderPh')"
                class="auth-header"
              />
              <a-checkbox v-model:checked="row.bearer">{{ t('servers.authBearer') }}</a-checkbox>
              <a-input-password v-model:value="row.value" :placeholder="valuePlaceholder(row)" class="auth-value" />
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

    <DeleteConfirmModal
      v-model:open="deleteVisible"
      :title="t('servers.deleteTitle')"
      :description="t('servers.deleteWarning')"
      :target="deleteTarget?.name"
      :requires-target="true"
      :typing-hint="t('servers.deleteTypingHint', { name: deleteTarget?.name })"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :loading="deleting"
      @confirm="confirmDelete"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { PlusOutlined, DeleteOutlined, ReloadOutlined, SearchOutlined, ApiOutlined } from '@ant-design/icons-vue'
import {
  createCredential,
  createServer,
  deleteCredential,
  deleteServer,
  getServerMetrics,
  listServerCredentialsAll,
  listServers,
  listServerToolsAll,
  primaryEndpoint,
  primaryTransport,
  testServer,
  testServerConnection,
  toggleServer,
  updateCredential,
  updateServer,
} from '@/api'
import DeleteConfirmModal from '@/components/DeleteConfirmModal.vue'
import type { Credential, MCPServer, MetricSnapshot, Transport } from '@/types'

const { t } = useI18n()

const servers = ref<MCPServer[]>([])
const toolCount = ref<Record<string, number>>({})
const loading = ref(false)
// 服务端分页状态：由后端 total 驱动，翻页/改页大小触发重新取数。
const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => t('filter.total', { total }),
})
// 列表筛选：q 关键词 + enabled/health 三态下拉。下拉未选(undefined)以便展示占位提示。
const filters = reactive<{ q: string; enabled?: string; health?: string }>({ q: '' })
const healthOptions = ['unknown', 'healthy', 'unhealthy', 'disabled']
const dialogVisible = ref(false)
const submitting = ref(false)
const testing = ref(false)
const deleteVisible = ref(false)
const deleteTarget = ref<MCPServer | null>(null)
const deleting = ref(false)
const form = reactive<{ name: string; endpoint: string; transport: Transport; description: string; args: string }>({
  name: '',
  endpoint: '',
  transport: 'https',
  description: '',
  args: '',
})
// stdio 传输：endpoint 承载可执行命令并展示启动参数输入框。
const isStdio = computed(() => form.transport === 'stdio')
// 上游请求头行：每条落库为一条 Credential（值加密存储）。
// credId 非空表示"已有凭证"——编辑时从后端加载，用于区分已有/新增，
// 避免每次保存都重复创建凭证；已有凭证留空密钥表示保留原值。
interface AuthRow {
  credId?: string
  name?: string
  header: string
  bearer: boolean
  value: string
  hasValue?: boolean
}
const reqHeaders = ref<AuthRow[]>([])
// 编辑态：true 时同一弹窗承担"编辑 Server（name 只读）+ 主实例连接参数 + 请求头"。
const editing = ref(false)
const editingId = ref('')
// 编辑时加载的已有凭证（用于删除判定：不在表单中的已有凭证 = 已删除）。
const loadedCreds = ref<Credential[]>([])
const authLoading = ref(false)
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
  { title: t('servers.endpoint'), key: 'endpoint', ellipsis: true },
  { title: t('servers.transport'), key: 'transport', width: 110 },
  { title: t('servers.instances'), key: 'instances', width: 90 },
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
    const res = await listServers({
      page: pagination.current,
      page_size: pagination.pageSize,
      q: filters.q.trim() || undefined,
      enabled: filters.enabled === 'true' ? true : filters.enabled === 'false' ? false : undefined,
      health_status: (filters.health as MCPServer['health_status']) || undefined,
    })
    servers.value = res.items
    pagination.total = res.total
    // 工具数只对当前页做 N+1（避免整表遍历）。
    const counts: Record<string, number> = {}
    for (const s of res.items) {
      try {
        counts[s.id] = (await listServerToolsAll(s.id)).length
      } catch {
        counts[s.id] = 0
      }
    }
    toolCount.value = counts
    if (res.items.length === 0 && pagination.current > 1 && res.total > 0) {
      pagination.current -= 1
      await load()
      return
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

function onFilterChange() {
  pagination.current = 1
  void load()
}

function onTableChange(p: { current?: number; pageSize?: number }) {
  if (p.current) pagination.current = p.current
  if (p.pageSize && p.pageSize !== pagination.pageSize) {
    pagination.pageSize = p.pageSize
    pagination.current = 1
  }
  void load()
}

function openCreate() {
  editing.value = false
  editingId.value = ''
  form.name = ''
  form.endpoint = ''
  form.transport = 'https'
  form.description = ''
  form.args = ''
  reqHeaders.value = []
  loadedCreds.value = []
  authLoading.value = false
  dialogVisible.value = true
}

// 编辑：name（展示名，可改）与 description 更新 Server；endpoint/transport/args 更新
// 主实例；请求头异步回显已保存凭证（密钥值不从后端返回，仅标记"已配置"）。
async function openEdit(row: MCPServer) {
  editing.value = true
  editingId.value = row.id
  const primary = row.instances?.[0]
  form.name = row.name
  form.description = row.description ?? ''
  form.endpoint = primary?.endpoint ?? ''
  form.transport = primary?.transport ?? 'https'
  form.args = primary?.args?.length ? JSON.stringify(primary.args) : ''
  reqHeaders.value = []
  loadedCreds.value = []
  dialogVisible.value = true
  authLoading.value = true
  try {
    const creds = await listServerCredentialsAll(row.id)
    loadedCreds.value = creds
    reqHeaders.value = creds.map(credToRow)
  } catch (e) {
    message.error(`${t('servers.authLoadFail')}: ${e}`)
  } finally {
    authLoading.value = false
  }
}

// credToRow 把已有凭证转换为表单行：api_key 显示真实 header 名；
// static_token 固定显示为 Authorization + Bearer。
function credToRow(cred: Credential): AuthRow {
  if (cred.kind === 'static_token') {
    return { credId: cred.id, name: cred.name, header: 'Authorization', bearer: true, value: '', hasValue: cred.has_value }
  }
  return { credId: cred.id, name: cred.name, header: cred.header ?? '', bearer: false, value: '', hasValue: cred.has_value }
}

// valuePlaceholder 提示已有凭证的留空语义：留空 = 保留原密钥。
function valuePlaceholder(row: AuthRow): string {
  if (row.credId && row.hasValue) return t('servers.authValueKeepPh')
  return t('servers.authValuePh')
}

function addAuthRow() {
  reqHeaders.value.push({ header: '', bearer: false, value: '' })
}

// rowToCredential 归一化一行请求头；bearer 行固定走 static_token（后端注入
// Authorization: Bearer <值>），其余为 api_key + 自定义 header 名。
function rowToCredential(row: AuthRow): { kind: 'api_key' | 'static_token'; header: string; value: string } | null {
  if (row.bearer) return { kind: 'static_token', header: '', value: row.value }
  const header = row.header.trim()
  if (!header) return null
  return { kind: 'api_key', header, value: row.value }
}

// buildTempHeaders 组装"保存前测试"用的临时请求头（仅包含本次输入了密钥的行；
// 已有凭证留空的行由后端按 server_id 加载已保存值）。
function buildTempHeaders(): Record<string, string> | null {
  const headers: Record<string, string> = {}
  for (const row of reqHeaders.value) {
    if (!row.value) continue
    if (row.bearer) {
      headers.Authorization = `Bearer ${row.value}`
      continue
    }
    const header = row.header.trim()
    if (!header) return null
    headers[header] = row.value
  }
  return headers
}

// parseStdioArgs 解析 stdio 启动参数（JSON 数组文本）；非法或非字符串数组时返回 null 供拦截。
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

// draftPayload 汇总当前表单为草稿拨测载荷（不落库）。
function draftPayload() {
  // 非 stdio 传输显式提交空数组，避免切换传输后残留 stdio 启动参数被后端拒绝。
  const args = isStdio.value ? parseStdioArgs(form.args) : []
  if (args === null) {
    message.warning(t('servers.argsInvalid'))
    return null
  }
  if (!form.endpoint.trim()) {
    message.warning(t('servers.fillRequired'))
    return null
  }
  const headers = buildTempHeaders()
  if (headers === null) {
    message.warning(t('servers.authHeaderRequired'))
    return null
  }
  return {
    server_id: editing.value ? editingId.value : undefined,
    name: form.name.trim() || undefined,
    endpoint: form.endpoint.trim(),
    transport: form.transport,
    args,
    headers,
  }
}

// testDraft 保存前测试连接：使用临时数据执行一次握手，不创建 Server/实例、
// 不刷新 Tools、不改健康状态、不保存临时请求头。
async function testDraft() {
  const payload = draftPayload()
  if (!payload) return
  testing.value = true
  try {
    await testServerConnection(payload)
    message.success(t('servers.testConnOk'))
  } catch (e) {
    message.error(`${t('servers.connectFail')}: ${e}`)
  } finally {
    testing.value = false
  }
}

async function submit() {
  if (!form.name.trim() || !form.endpoint.trim()) {
    message.warning(t('servers.fillRequired'))
    return
  }
  // 非 stdio 传输提交空数组：切换传输时清掉遗留的 stdio 启动参数。
  const args = isStdio.value ? parseStdioArgs(form.args) : []
  if (args === null) {
    message.warning(t('servers.argsInvalid'))
    return
  }
  // 校验请求头行（api_key 必须有 header 名）；完全空白的行忽略。
  const rows: { row: AuthRow; cred: { kind: 'api_key' | 'static_token'; header: string; value: string } }[] = []
  for (const row of reqHeaders.value) {
    if (!row.credId && !row.value && !row.bearer && !row.header.trim()) continue
    const cred = rowToCredential(row)
    if (cred === null) {
      message.warning(t('servers.authHeaderRequired'))
      return
    }
    rows.push({ row, cred })
  }

  submitting.value = true
  try {
    if (editing.value) {
      await saveEdit(args, rows)
    } else {
      await saveCreate(args, rows)
    }
    dialogVisible.value = false
    await load()
  } catch (e) {
    message.error(String(e))
  } finally {
    submitting.value = false
  }
}

// saveCreate 新增：先建逻辑 Server + 主实例，再逐条落凭证（值加密存储）。
async function saveCreate(
  args: string[],
  rows: { row: AuthRow; cred: { kind: 'api_key' | 'static_token'; header: string; value: string } }[],
) {
  const server = await createServer({
    name: form.name.trim(),
    description: form.description,
    endpoint: form.endpoint.trim(),
    transport: form.transport,
    args,
  })
  const failures: string[] = []
  let created = 0
  for (const [i, { cred }] of rows.entries()) {
    if (!cred.value) continue // 无密钥值 → 不创建空凭证
    try {
      await createCredential(server.id, { name: `${server.name} auth#${i + 1}`, ...cred })
      created += 1
    } catch (e) {
      failures.push(String(e))
    }
  }
  if (failures.length) {
    message.warning(`${t('servers.registeredOk')}，${t('servers.authFail')}: ${failures[0]}`)
  } else if (created) {
    message.success(`${t('servers.registeredOk')}，${t('servers.authApplied')}`)
  } else {
    message.success(t('servers.registeredOk'))
  }
}

// saveEdit 编辑：description 更新 Server；endpoint/transport/args 更新主实例；
// 请求头按"已有凭证（留空保留原值 / 填值替换）/ 新增行（创建）/ 删除行（按 ID 删除）"
// 三类动作分别落库，避免重复创建已有凭证。
async function saveEdit(
  args: string[],
  rows: { row: AuthRow; cred: { kind: 'api_key' | 'static_token'; header: string; value: string } }[],
) {
  await updateServer(editingId.value, {
    name: form.name.trim(),
    description: form.description,
    endpoint: form.endpoint.trim(),
    transport: form.transport,
    args,
  })

  const failures: string[] = []
  const keptIds = new Set(rows.map(({ row }) => row.credId).filter((id): id is string => Boolean(id)))
  // 表单中已移除的已有凭证 → 按 Credential ID 删除。
  for (const cred of loadedCreds.value) {
    if (keptIds.has(cred.id)) continue
    try {
      await deleteCredential(editingId.value, cred.id)
    } catch (e) {
      failures.push(String(e))
    }
  }
  for (const [i, { row, cred }] of rows.entries()) {
    try {
      if (row.credId) {
        const original = loadedCreds.value.find((c) => c.id === row.credId)
        const headerChanged = (original?.header ?? '') !== cred.header || original?.kind !== cred.kind
        // 已有凭证：密钥留空表示保留原值；未改名/未换类型时无需请求。
        if (!cred.value && !headerChanged) continue
        const payload: { name?: string; kind: 'api_key' | 'static_token'; header?: string; value?: string } = {
          name: original?.name ?? `${form.name} auth#${i + 1}`,
          kind: cred.kind,
        }
        if (cred.header) payload.header = cred.header
        if (cred.value) payload.value = cred.value
        await updateCredential(editingId.value, row.credId, payload)
        continue
      }
      // 新增行：无密钥值则不创建。
      if (!cred.value) continue
      await createCredential(editingId.value, { name: `${form.name} auth#${i + 1}`, ...cred })
    } catch (e) {
      failures.push(String(e))
    }
  }
  if (failures.length) {
    message.warning(`${t('servers.updatedOk')}，${t('servers.authReconcileFail')}: ${failures[0]}`)
  } else {
    message.success(t('servers.updatedOk'))
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

function requestDelete(row: MCPServer) {
  deleteTarget.value = row
  deleteVisible.value = true
}

async function confirmDelete() {
  const row = deleteTarget.value
  if (!row) return
  deleting.value = true
  try {
    await deleteServer(row.id)
    message.success(t('servers.deletedOk'))
    deleteVisible.value = false
    deleteTarget.value = null
    await load()
  } catch (e) {
    message.error(String(e))
  } finally {
    deleting.value = false
  }
}

const healthColor = (s: string) => (s === 'healthy' ? 'green' : s === 'unhealthy' ? 'red' : 'default')
</script>

<style scoped>
.toolbar {
  margin-bottom: 16px;
}
.filters {
  margin-bottom: 12px;
}
.link {
  color: var(--mc-accent-light);
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
  color: var(--mc-ink-3);
  font-size: 12px;
  padding: 2px 0 8px;
}
.field-hint {
  display: block;
  color: var(--mc-ink-3);
  font-size: 12px;
  line-height: 18px;
  margin-top: 4px;
}
.edit-hint {
  color: var(--mc-ink-3);
  font-size: 12px;
}
</style>
