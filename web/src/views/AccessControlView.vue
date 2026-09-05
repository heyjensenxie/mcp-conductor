<template>
  <a-spin :spinning="loading">
    <a-tabs v-model:activeKey="tab" @change="onTabChange">
      <!-- ==================== API Keys ==================== -->
      <a-tab-pane :key="'keys'" :tab="t('access.tabKeys')">
        <a-alert
          v-if="authRequired === false"
          type="warning"
          show-icon
          class="mb"
          :message="t('access.authDisabled')"
          :description="t('access.authDisabledDesc')"
        />
        <div class="toolbar">
          <a-button type="primary" @click="openCreate">
            <template #icon><PlusOutlined /></template>{{ t('access.newKey') }}
          </a-button>
        </div>

        <a-alert
          v-if="!keysLoading && keys.length === 0"
          type="info"
          show-icon
          class="mb"
          :message="t('access.tabKeys')"
          :description="t('access.noKeysDesc')"
        />

        <a-table :data-source="keys" :columns="keyColumns" :loading="keysLoading" :pagination="false" :row-key="(r: any) => r.id">
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'enabled'">
              <a-tag :color="record.enabled ? 'green' : 'default'">{{ record.enabled ? t('common.yes') : t('common.no') }}</a-tag>
            </template>
            <template v-else-if="column.key === 'quota'">
              <span>{{ record.qps > 0 ? `${record.qps}/s, burst ${record.burst}` : '—' }}</span>
            </template>
            <template v-else-if="column.key === 'tools_count'">
              <span>{{ record.grants?.length ?? 0 }}</span>
            </template>
            <template v-else-if="column.key === 'actions'">
              <a-button size="small" type="link" @click="openEdit(record)">{{ t('access.edit') }}</a-button>
              <a-button size="small" type="link" danger @click="remove(record)">
                <template #icon><DeleteOutlined /></template>
              </a-button>
            </template>
          </template>
        </a-table>
      </a-tab-pane>

      <!-- ==================== 遗留策略规则 ==================== -->
      <a-tab-pane :key="'policies'" :tab="t('access.tabPolicies')">
        <div class="toolbar">
          <a-button type="primary" @click="openPolicyDialog()">
            <template #icon><PlusOutlined /></template>{{ t('access.newPolicy') }}
          </a-button>
        </div>
        <a-table :data-source="policies" :columns="policyColumns" :loading="policiesLoading" :pagination="false" :row-key="(r: any) => r.id">
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'rules'">
              <a-tag v-for="rule in record.rules" :key="rule.tool + rule.subject" :color="rule.effect === 'deny' ? 'red' : 'green'" class="rule-tag">
                {{ rule.subject }} → {{ rule.tool }} [{{ rule.effect }}]
              </a-tag>
            </template>
            <template v-else-if="column.key === 'enabled'">
              <a-switch :checked="record.enabled" @change="(checked: boolean) => togglePolicyRow(record, checked)" />
            </template>
            <template v-else-if="column.key === 'actions'">
              <a-space :size="4">
                <a-button size="small" @click="openPolicyDialog(record)">{{ t('common.edit') }}</a-button>
                <a-popconfirm :title="t('access.policyConfirmDelete')" @confirm="removePolicy(record)">
                  <a-button size="small" danger>{{ t('common.delete') }}</a-button>
                </a-popconfirm>
              </a-space>
            </template>
          </template>
        </a-table>
      </a-tab-pane>
    </a-tabs>

    <!-- 新建 Key -->
    <a-modal v-model:open="createVisible" :title="t('access.newKey')" :ok-text="t('common.create')" :cancel-text="t('common.cancel')" :confirm-loading="creating" @ok="submitCreate">
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item :label="t('access.name')" :required="true">
          <a-input v-model:value="createForm.name" />
        </a-form-item>
        <a-form-item :label="t('access.subject')" :required="true">
          <a-input v-model:value="createForm.subject" placeholder="partner-a" />
        </a-form-item>
        <a-form-item :label="t('access.qps')">
          <a-input-number v-model:value="createForm.qps" :min="0" :style="{ width: '100%' }" :placeholder="t('access.qupPlaceholder')" />
        </a-form-item>
        <a-form-item :label="t('access.burst')">
          <a-input-number v-model:value="createForm.burst" :min="0" :style="{ width: '100%' }" :placeholder="t('access.burstPlaceholder')" />
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 一次性密钥展示 -->
    <a-modal v-model:open="secretVisible" :title="t('access.secret')" :footer="null" width="560px">
      <a-alert type="warning" show-icon class="mb" :message="t('access.secretOnce')" />
      <a-input-group compact style="display: flex">
        <a-input :value="createdSecret" read-only class="secret-input" />
        <a-button @click="copySecret">
          <template #icon><CopyOutlined /></template>{{ t('access.copy') }}
        </a-button>
      </a-input-group>
    </a-modal>

    <!-- Key 配置 / 工具授权编辑器 -->
    <a-modal
      v-model:open="editVisible"
      :title="`${t('access.grantEditor')} — ${editForm.subject}`"
      :ok-text="t('access.saveGrant')"
      :cancel-text="t('common.cancel')"
      :confirm-loading="saving"
      :width="880"
      @ok="saveEdit"
    >
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }" class="mb">
        <a-form-item :label="t('access.name')">
          <a-input v-model:value="editForm.name" />
        </a-form-item>
        <a-form-item :label="t('access.qps')">
          <a-input-number v-model:value="editForm.qps" :min="0" :style="{ width: 140 }" />
          <a-form-item-rest />
          <span class="inline-label">Burst</span>
          <a-input-number v-model:value="editForm.burst" :min="0" :style="{ width: 140, marginLeft: 8 }" />
        </a-form-item>
        <a-form-item :label="t('access.enabled')">
          <a-switch v-model:checked="editForm.enabled" />
        </a-form-item>
      </a-form>

      <div class="grant-layout">
        <!-- 左侧：可用工具（跨 Server 聚合） -->
        <div class="grant-available">
          <div class="grant-title">{{ t('access.availableTools') }}</div>
          <div v-if="servers.length === 0" class="server-empty">{{ t('access.noServersHint') }}</div>
          <div v-for="server in servers" :key="server.id" class="server-group">
            <div class="server-name">{{ server.name }}</div>
            <div v-for="tool in toolsOf(server.id)" :key="tool.gateway_name" class="tool-row">
              <a-checkbox
                :checked="isGranted(tool.gateway_name)"
                @change="(e: any) => toggleTool(tool.gateway_name, e.target.checked)"
              >
                <span class="tool-name">{{ tool.gateway_name }}</span>
              </a-checkbox>
            </div>
            <div v-if="toolsOf(server.id).length === 0" class="server-empty">—</div>
          </div>
          <div class="pattern-row">
            <span class="pattern-label">{{ t('access.patternLabel') }}</span>
            <a-input v-model:value="patternInput" size="small" :placeholder="t('access.patternPlaceholder')" @press-enter="addPattern" />
            <a-button size="small" @click="addPattern">{{ t('access.addPattern') }}</a-button>
          </div>
        </div>

        <!-- 右侧：已授权 + 调用配置 -->
        <div class="grant-config">
          <div class="grant-title">{{ t('access.granted') }} ({{ editGrants.length }})</div>
          <div v-if="editGrants.length === 0" class="server-empty">{{ t('access.noGranted') }}</div>
          <div v-for="(grant, gi) in editGrants" :key="grant.gw" class="grant-card">
            <div class="grant-card-head">
              <a-typography-text code>{{ grant.gw }}</a-typography-text>
              <a-tag v-if="grant.pattern" color="blue">*</a-tag>
              <a-button size="small" type="link" danger @click="editGrants.splice(gi, 1)">
                <template #icon><DeleteOutlined /></template>
              </a-button>
            </div>

            <div class="kv-section">
              <span class="kv-label">{{ t('access.headers') }}</span>
              <div v-for="(h, hi) in grant.headers" :key="hi" class="kv-row">
                <a-input v-model:value="h.k" size="small" :placeholder="t('access.headerKey')" class="kv-k" />
                <a-input v-model:value="h.v" size="small" :placeholder="t('access.headerValue')" class="kv-v" />
                <a-button size="small" type="text" @click="grant.headers.splice(hi, 1)">
                  <template #icon><DeleteOutlined /></template>
                </a-button>
              </div>
              <a-button size="small" type="dashed" @click="grant.headers.push({ k: '', v: '' })">
                <template #icon><PlusOutlined /></template>{{ t('access.addHeader') }}
              </a-button>
            </div>

            <div class="kv-section">
              <span class="kv-label">{{ t('access.defaultArgs') }}</span>
              <div v-for="(a, ai) in grant.defargs" :key="ai" class="kv-row">
                <a-input v-model:value="a.k" size="small" :placeholder="t('access.argKey')" class="kv-k" />
                <a-input v-model:value="a.v" size="small" :placeholder="t('access.argValue')" class="kv-v" />
                <a-button size="small" type="text" @click="grant.defargs.splice(ai, 1)">
                  <template #icon><DeleteOutlined /></template>
                </a-button>
              </div>
              <a-button size="small" type="dashed" @click="grant.defargs.push({ k: '', v: '' })">
                <template #icon><PlusOutlined /></template>{{ t('access.addArg') }}
              </a-button>
            </div>
          </div>
        </div>
      </div>
    </a-modal>

    <!-- 遗留策略弹窗（创建/编辑共用） -->
    <a-modal v-model:open="policyDialogVisible" :title="editingPolicyId ? t('access.policyEditTitle') : t('access.newPolicy')" :ok-text="editingPolicyId ? t('common.save') : t('access.create')" :cancel-text="t('common.cancel')" width="600px" @ok="submitPolicy">
      <a-alert type="info" show-icon class="mb">{{ t('access.formatHint') }}</a-alert>
      <a-form :label-col="{ span: 4 }" :wrapper-col="{ span: 20 }">
        <a-form-item :label="t('access.name')" :required="true">
          <a-input v-model:value="policyForm.name" />
        </a-form-item>
        <a-form-item :label="t('access.rules')">
          <a-textarea v-model:value="rulesText" :rows="5" placeholder="agent-a|payment.create|deny" />
        </a-form-item>
      </a-form>
    </a-modal>
  </a-spin>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { CopyOutlined, DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue'
import {
  createKey, createPolicy, deleteKey, deletePolicy, getAuthStatus, listKeys, listPolicies, listServers, listTools,
  togglePolicy, updateKey, updatePolicy,
} from '@/api'
import type { AccessKey, MCPServer, Policy, PolicyRule, Tool, ToolGrant } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const authRequired = ref<boolean | null>(null) // 认证是否开启（决定白名单/配额是否生效）

// ---- keys ----
const tab = ref<'keys' | 'policies'>('keys')
const keys = ref<AccessKey[]>([])
const keysLoading = ref(false)

const keyColumns = computed<any[]>(() => [
  { title: t('access.name'), key: 'name', dataIndex: 'name' },
  { title: t('access.subject'), key: 'subject', dataIndex: 'subject' },
  { title: t('access.qps').split(' ')[0], key: 'quota', width: 120 },
  { title: t('access.toolsCount'), key: 'tools_count', width: 90 },
  { title: t('access.enabled'), key: 'enabled', width: 90 },
  { title: t('access.actions'), key: 'actions', width: 180 },
])

const createVisible = ref(false)
const creating = ref(false)
const createForm = reactive({ name: '', subject: '', qps: 0, burst: 0 })

const secretVisible = ref(false)
const createdSecret = ref('')

function openCreate() {
  createForm.name = ''
  createForm.subject = ''
  createForm.qps = 0
  createForm.burst = 0
  createVisible.value = true
}

async function submitCreate() {
  if (!createForm.name || !createForm.subject) {
    message.warning(t('access.subjectRequired'))
    return
  }
  creating.value = true
  try {
    const key = await createKey({
      name: createForm.name,
      subject: createForm.subject,
      qps: createForm.qps,
      burst: createForm.burst,
      grants: [],
    })
    createVisible.value = false
    createdSecret.value = key.secret ?? ''
    secretVisible.value = true
    message.success(t('access.generatedOk'))
    await loadKeys()
  } catch (e) {
    message.error(String(e))
  } finally {
    creating.value = false
  }
}

async function copySecret() {
  try {
    await navigator.clipboard.writeText(createdSecret.value)
    message.success(t('access.copied'))
  } catch {
    message.warning(t('access.copyFailed'))
  }
}

async function remove(key: AccessKey) {
  if (!window.confirm(t('common.confirmDelete'))) return
  try {
    await deleteKey(key.id)
    message.success(t('access.deletedOk'))
    await loadKeys()
  } catch (e) {
    message.error(String(e))
  }
}

async function loadKeys() {
  keysLoading.value = true
  try {
    keys.value = await listKeys()
  } catch (e) {
    message.error(String(e))
  } finally {
    keysLoading.value = false
  }
}

// ---- grants editor ----
interface GrantRow {
  gw: string
  pattern: boolean
  headers: { k: string; v: string }[]
  defargs: { k: string; v: string }[]
}

const editVisible = ref(false)
const saving = ref(false)
const editKey = ref<AccessKey | null>(null)
const editForm = reactive({ name: '', subject: '', qps: 0, burst: 0, enabled: true })
const editGrants = ref<GrantRow[]>([])
const patternInput = ref('')
const servers = ref<MCPServer[]>([])
const toolsAll = ref<Tool[]>([])

async function loadTools() {
  try {
    servers.value = await listServers()
    toolsAll.value = await listTools()
  } catch (e) {
    message.error(String(e))
  }
}

function toolsOf(serverId: string): Tool[] {
  return toolsAll.value.filter((tool) => tool.server_id === serverId)
}

function isGranted(gw: string): boolean {
  return editGrants.value.some((g) => g.gw === gw)
}

function toggleTool(gw: string, checked: boolean) {
  if (checked) {
    if (!isGranted(gw)) editGrants.value.push({ gw, pattern: false, headers: [], defargs: [] })
  } else {
    editGrants.value = editGrants.value.filter((g) => g.gw !== gw)
  }
}

function addPattern() {
  const gw = patternInput.value.trim()
  if (!gw) return
  if (!/^([\w-]+)\.\*$|^\*$/.test(gw)) {
    message.warning(t('access.patternPlaceholder'))
    return
  }
  if (!isGranted(gw)) editGrants.value.push({ gw, pattern: true, headers: [], defargs: [] })
  patternInput.value = ''
}

function openEdit(key: AccessKey) {
  editKey.value = key
  editForm.name = key.name
  editForm.subject = key.subject
  editForm.qps = key.qps
  editForm.burst = key.burst
  editForm.enabled = key.enabled
  editGrants.value = key.grants.map((g) => ({
    gw: g.gateway_name,
    pattern: !toolsAll.value.some((tool) => tool.gateway_name === g.gateway_name),
    headers: Object.entries(g.headers ?? {}).map(([k, v]) => ({ k, v })),
    defargs: Object.entries(g.default_args ?? {}).map(([k, v]) => ({ k, v: JSON.stringify(v) })),
  }))
  editVisible.value = true
}

// parseArg 把编辑文本解析为参数值：合法 JSON 按类型，否则视为字符串。
function parseArg(raw: string): unknown {
  const trimmed = raw.trim()
  if (trimmed === '') return ''
  try {
    return JSON.parse(trimmed)
  } catch {
    return raw
  }
}

async function saveEdit() {
  if (!editKey.value) return
  const grants: ToolGrant[] = editGrants.value.map((g) => {
    const headers: Record<string, string> = {}
    for (const h of g.headers) if (h.k) headers[h.k] = h.v
    const defaultArgs: Record<string, unknown> = {}
    for (const a of g.defargs) if (a.k) defaultArgs[a.k] = parseArg(a.v)
    const grant: ToolGrant = { gateway_name: g.gw }
    if (Object.keys(headers).length) grant.headers = headers
    if (Object.keys(defaultArgs).length) grant.default_args = defaultArgs
    return grant
  })
  saving.value = true
  try {
    await updateKey(editKey.value.id, {
      name: editForm.name,
      enabled: editForm.enabled,
      qps: editForm.qps,
      burst: editForm.burst,
      grants,
    })
    editVisible.value = false
    message.success(t('access.savedOk'))
    await loadKeys()
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}

// ---- legacy policies ----
const policies = ref<Policy[]>([])
const policiesLoading = ref(false)
const policyDialogVisible = ref(false)
const editingPolicyId = ref('')
const policyForm = reactive({ name: '' })
const rulesText = ref('')

const policyColumns = computed<any[]>(() => [
  { title: t('access.name'), key: 'name', dataIndex: 'name' },
  { title: t('access.rules'), key: 'rules' },
  { title: t('access.enabled'), key: 'enabled', dataIndex: 'enabled', width: 90 },
  { title: t('common.actions'), key: 'actions', width: 170 },
])

async function loadPolicies() {
  policiesLoading.value = true
  try {
    policies.value = await listPolicies()
  } catch (e) {
    message.error(String(e))
  } finally {
    policiesLoading.value = false
  }
}

// parseRulesText 把多行 "subject|tool|effect" 文本解析为规则列表。
function parseRulesText(): PolicyRule[] {
  return rulesText.value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [subject, tool, effect] = line.split('|').map((s) => s.trim())
      return { subject, tool, effect: (effect === 'deny' ? 'deny' : 'allow') as PolicyRule['effect'] }
    })
}

// openPolicyDialog 新建或编辑策略（record 缺省为新建）。
function openPolicyDialog(record?: Policy) {
  editingPolicyId.value = record?.id ?? ''
  policyForm.name = record?.name ?? ''
  rulesText.value = record ? record.rules.map((r) => `${r.subject}|${r.tool}|${r.effect}`).join('\n') : ''
  policyDialogVisible.value = true
}

async function submitPolicy() {
  if (!policyForm.name) {
    message.warning(t('access.nameRequired'))
    return
  }
  const rules = parseRulesText()
  try {
    if (editingPolicyId.value) {
      await updatePolicy(editingPolicyId.value, { name: policyForm.name, rules })
      message.success(t('access.policyUpdatedOk'))
    } else {
      await createPolicy({ name: policyForm.name, enabled: true, rules })
      message.success(t('access.createdOk'))
    }
    policyDialogVisible.value = false
    rulesText.value = ''
    await loadPolicies()
  } catch (e) {
    message.error(String(e))
  }
}

async function togglePolicyRow(record: Policy, enabled: boolean) {
  try {
    await togglePolicy(record.id, enabled)
    await loadPolicies()
  } catch (e) {
    message.error(String(e))
  }
}

async function removePolicy(record: Policy) {
  try {
    await deletePolicy(record.id)
    message.success(t('access.policyDeletedOk'))
    await loadPolicies()
  } catch (e) {
    message.error(String(e))
  }
}

function onTabChange(key: string) {
  if (key === 'policies' && policies.value.length === 0) loadPolicies()
}

onMounted(async () => {
  loading.value = true
  try {
    await Promise.all([loadKeys(), loadTools(), loadAuthStatus()])
  } finally {
    loading.value = false
  }
})

async function loadAuthStatus() {
  try {
    const status = await getAuthStatus()
    authRequired.value = status.auth_required
  } catch {
    /* 免认证端点，通常不会失败 */
  }
}
</script>

<style scoped>
.toolbar {
  margin-bottom: 16px;
}
.rule-tag {
  margin-right: 6px;
}
.mb {
  margin-bottom: 12px;
}
.secret-input {
  font-family: monospace;
}
.inline-label {
  margin-left: 16px;
  color: #888;
}
/* 授权编辑器双栏布局 */
.grant-layout {
  display: flex;
  gap: 16px;
}
.grant-available {
  flex: 0 0 250px;
  max-height: 460px;
  overflow: auto;
  border: 1px solid #eee;
  border-radius: 6px;
  padding: 8px;
}
.grant-config {
  flex: 1;
  max-height: 460px;
  overflow: auto;
}
.grant-title {
  font-weight: 600;
  margin-bottom: 8px;
}
.server-group {
  margin-bottom: 10px;
}
.server-name {
  font-size: 12px;
  color: #999;
  margin: 6px 0 2px;
}
.tool-row {
  padding: 1px 0;
}
.tool-name {
  font-size: 13px;
}
.server-empty {
  color: #bbb;
  font-size: 12px;
  padding: 6px 0;
}
.pattern-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  border-top: 1px dashed #eee;
  padding-top: 8px;
}
.pattern-label {
  font-size: 12px;
  color: #999;
  white-space: nowrap;
}
.grant-card {
  border: 1px solid #eee;
  border-radius: 6px;
  padding: 8px 10px;
  margin-bottom: 10px;
}
.grant-card-head {
  display: flex;
  align-items: center;
  gap: 6px;
}
.kv-section {
  margin-top: 8px;
}
.kv-label {
  display: block;
  font-size: 12px;
  color: #999;
  margin-bottom: 4px;
}
.kv-row {
  display: flex;
  gap: 6px;
  margin-bottom: 4px;
}
.kv-k {
  width: 120px;
}
.kv-v {
  flex: 1;
}
</style>