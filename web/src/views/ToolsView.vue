<template>
  <a-card :bordered="true">
    <a-alert type="info" show-icon class="hint" :message="t('tools.catalogHint')" />
    <div class="filters">
      <a-space wrap :size="8">
        <a-input v-model:value="filters.q" allow-clear :placeholder="t('filter.keyword')" style="width: 240px" @press-enter="onFilterChange">
          <template #prefix><SearchOutlined /></template>
        </a-input>
        <a-select v-model:value="filters.serverId" allow-clear :placeholder="t('filter.serverPlaceholder')" style="width: 200px" @change="onFilterChange">
          <a-select-option v-for="s in servers" :key="s.id" :value="s.id">{{ s.name }}</a-select-option>
        </a-select>
        <a-select v-model:value="filters.enabled" allow-clear :placeholder="t('filter.statusPlaceholder')" style="width: 120px" @change="onFilterChange">
          <a-select-option value="true">{{ t('filter.enabled') }}</a-select-option>
          <a-select-option value="false">{{ t('filter.disabled') }}</a-select-option>
        </a-select>
      </a-space>
    </div>
    <a-table :data-source="tools" :columns="columns" :loading="loading" :pagination="pagination" @change="onTableChange" :row-key="(r: any) => r.id">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'gateway_name'">
          <a-space><a-typography-text code>{{ record.gateway_name }}</a-typography-text><a-tag v-if="record.name_overridden" color="blue">{{ t('tools.customized') }}</a-tag></a-space>
        </template>
        <template v-else-if="column.key === 'actions'">
          <a-space><a-button size="small" @click="openEdit(record)">{{ t('common.edit') }}</a-button><a-switch :checked="record.enabled" :loading="toggling === record.id" @change="(checked: boolean) => toggle(record, checked)" /></a-space>
        </template>
      </template>
    </a-table>
  </a-card>

  <a-modal v-model:open="editVisible" :title="t('tools.editTitle')" :confirm-loading="saving" :width="760" @ok="save">
    <a-form layout="vertical">
      <a-form-item :label="t('tools.original')"><a-input :value="editing?.original_name" disabled /></a-form-item>
      <a-form-item :label="t('tools.gatewayName')" :help="t('tools.renameHint')">
        <a-input v-model:value="form.gatewayName" />
        <a-button v-if="editing?.name_overridden" type="link" size="small" class="reset" @click="resetName = true; form.gatewayName = canonicalName">{{ t('tools.resetSource') }}</a-button>
      </a-form-item>
      <a-form-item :label="t('tools.description')">
        <a-textarea v-model:value="form.description" :rows="3" />
        <a-button v-if="editing?.description_overridden" type="link" size="small" class="reset" @click="resetDescription = true; form.description = editing?.source_description ?? ''">{{ t('tools.resetSource') }}</a-button>
      </a-form-item>
      <a-form-item :label="t('tools.inputSchema')" :help="t('tools.schemaHint')">
        <a-textarea v-model:value="form.inputSchema" :rows="12" class="schema" />
        <a-button v-if="editing?.input_schema_overridden" type="link" size="small" class="reset" @click="resetSchemaToSource">{{ t('tools.resetSource') }}</a-button>
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { SearchOutlined } from '@ant-design/icons-vue'
import { listAllServers, listTools, toggleTool, updateTool } from '@/api'
import type { MCPServer, Tool } from '@/types'

const { t } = useI18n()
const tools = ref<Tool[]>([])
const servers = ref<MCPServer[]>([])
const loading = ref(false)
// 服务端分页状态。
const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => t('filter.total', { total }),
})
const filters = reactive({ q: '', serverId: '', enabled: '' })
const toggling = ref('')
const editVisible = ref(false)
const saving = ref(false)
const editing = ref<Tool | null>(null)
const resetName = ref(false)
const resetDescription = ref(false)
const resetInputSchema = ref(false)
const form = reactive({ gatewayName: '', description: '', inputSchema: '' })

const columns = computed<any[]>(() => [
  { title: t('tools.gatewayName'), key: 'gateway_name', dataIndex: 'gateway_name' },
  { title: t('tools.original'), key: 'original_name', dataIndex: 'original_name', width: 150 },
  { title: t('tools.server'), key: 'server_id', dataIndex: 'server_id', width: 140 },
  { title: t('tools.description'), key: 'description', dataIndex: 'description', ellipsis: true },
  { title: t('common.actions'), key: 'actions', width: 150 },
])

const canonicalName = computed(() => {
  const server = servers.value.find((item) => item.id === editing.value?.server_id)
  const namespace = (server?.name ?? 'server').toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '') || 'server'
  return `${namespace}.${editing.value?.original_name ?? ''}`
})

onMounted(async () => {
  // servers 全量只拉一次：供 server 列与 reset 名解析；表格工具本身按需分页。
  try {
    servers.value = await listAllServers()
  } catch {
    servers.value = []
  }
  await load()
})

async function load() {
  loading.value = true
  try {
    const res = await listTools({
      page: pagination.current,
      page_size: pagination.pageSize,
      q: filters.q.trim() || undefined,
      server_id: filters.serverId || undefined,
      enabled: filters.enabled === 'true' ? true : filters.enabled === 'false' ? false : undefined,
    })
    tools.value = res.items
    pagination.total = res.total
    if (res.items.length === 0 && pagination.current > 1 && res.total > 0) {
      pagination.current -= 1
      await load()
      return
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

function openEdit(tool: Tool) {
  editing.value = tool
  form.gatewayName = tool.gateway_name
  form.description = tool.description ?? ''
  form.inputSchema = JSON.stringify(tool.input_schema ?? {}, null, 2)
  resetName.value = resetDescription.value = resetInputSchema.value = false
  editVisible.value = true
}

function resetSchemaToSource() {
  resetInputSchema.value = true
  form.inputSchema = JSON.stringify(editing.value?.source_input_schema ?? {}, null, 2)
}

async function save() {
  if (!editing.value) return
  let schema: Record<string, unknown>
  try { schema = JSON.parse(form.inputSchema) }
  catch { message.warning(t('tools.invalidSchema')); return }
  saving.value = true
  try {
    await updateTool(editing.value.id, { gateway_name: form.gatewayName, description: form.description, input_schema: schema, reset_name: resetName.value, reset_description: resetDescription.value, reset_input_schema: resetInputSchema.value })
    editVisible.value = false
    message.success(t('tools.updatedOk'))
    await load()
  } catch (e) { message.error(String(e)) }
  finally { saving.value = false }
}

async function toggle(record: Tool, enabled: boolean) {
  toggling.value = record.id
  try { await toggleTool(record.id, enabled); message.success(t('tools.toggledOk')); await load() }
  catch (e) { message.error(String(e)) }
  finally { toggling.value = '' }
}
</script>

<style scoped>
.hint { margin-bottom: 16px; }
.filters { margin-bottom: 12px; }
.schema { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.reset { padding-left: 0; }
</style>
