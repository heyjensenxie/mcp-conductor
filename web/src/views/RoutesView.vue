<template>
  <div>
    <div class="toolbar">
      <a-space>
        <a-button type="primary" @click="openCreate">
          <template #icon><PlusOutlined /></template>{{ t('routes.newRoute') }}
        </a-button>
        <a-button @click="load">
          <template #icon><ReloadOutlined /></template>{{ t('routes.refresh') }}
        </a-button>
      </a-space>
    </div>

    <a-card :bordered="true">
      <div class="filters">
        <a-space wrap :size="8">
          <a-input v-model:value="filters.q" allow-clear :placeholder="t('filter.keyword')" style="width: 220px" @press-enter="onFilterChange">
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
      <a-table :data-source="routes" :columns="columns" :loading="loading" :pagination="pagination" @change="onTableChange" :row-key="(r: any) => r.id">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'tools'">
            <a-tag v-for="tool in record.tool_names || []" :key="tool" class="tool-tag">{{ tool }}</a-tag>
            <span v-if="!(record.tool_names || []).length">-</span>
          </template>
          <template v-else-if="column.key === 'enabled'">
            <a-switch :checked="record.enabled" @change="(checked: boolean) => toggle(record, checked)" />
          </template>
          <template v-else-if="column.key === 'actions'">
            <a-space :size="4">
              <a-button size="small" @click="openEdit(record)">{{ t('common.edit') }}</a-button>
              <a-popconfirm :title="t('routes.confirmDelete')" @confirm="remove(record)">
                <a-button size="small" danger>{{ t('common.delete') }}</a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal
      v-model:open="dialogVisible"
      :title="editingId ? t('routes.editTitle') : t('routes.newRoute')"
      :ok-text="editingId ? t('routes.save') : t('routes.create')"
      :cancel-text="t('common.cancel')"
      :confirm-loading="saving"
      @ok="submit"
    >
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item :label="t('routes.name')" :required="true">
          <a-input v-model:value="form.name" />
        </a-form-item>
        <a-form-item :label="t('routes.serverId')" :required="true">
          <a-select v-model:value="form.server_id" :placeholder="t('routes.selectServerPlaceholder')" show-search option-filter-prop="label">
            <a-select-option v-for="s in servers" :key="s.id" :value="s.id" :label="s.name">{{ s.name }} ({{ s.endpoint }})</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="t('routes.toolNames')">
          <a-select v-model:value="form.tool_names" mode="multiple" :placeholder="t('routes.toolPlaceholder')" option-filter-prop="label" allow-clear>
            <a-select-option v-for="tool in allTools" :key="tool" :value="tool" :label="tool">{{ tool }}</a-select-option>
          </a-select>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { PlusOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons-vue'
import { createRoute, deleteRoute, listAllServers, listAllTools, listRoutes, toggleRoute, updateRoute } from '@/api'
import type { MCPServer, Route } from '@/types'

const { t } = useI18n()
const routes = ref<Route[]>([])
const servers = ref<MCPServer[]>([])
const allTools = ref<string[]>([])
const loading = ref(false)
const pagination = reactive({
  current: 1,
  pageSize: 20,
  total: 0,
  showSizeChanger: true,
  showTotal: (total: number) => t('filter.total', { total }),
})
const filters = reactive({ q: '', serverId: '', enabled: '' })
const dialogVisible = ref(false)
const saving = ref(false)
const editingId = ref('')
const form = reactive<{ name: string; server_id: string; tool_names: string[] }>({
  name: '',
  server_id: '',
  tool_names: [],
})

const columns = computed<any[]>(() => [
  { title: t('routes.name'), key: 'name', dataIndex: 'name' },
  { title: t('routes.serverId'), key: 'server_id', dataIndex: 'server_id', width: 180 },
  { title: t('routes.tools'), key: 'tools' },
  { title: t('routes.enabled'), key: 'enabled', dataIndex: 'enabled', width: 90 },
  { title: t('common.actions'), key: 'actions', width: 160 },
])

onMounted(async () => {
  // 弹层选项（servers / 全部工具）全量拉取一次；路由表按需分页。
  try {
    const [serverList, toolList] = await Promise.all([listAllServers(), listAllTools()])
    servers.value = serverList
    allTools.value = toolList.map((tool) => tool.gateway_name)
  } catch (e) {
    message.error(String(e))
  }
  await load()
})

async function load() {
  loading.value = true
  try {
    const res = await listRoutes({
      page: pagination.current,
      page_size: pagination.pageSize,
      q: filters.q.trim() || undefined,
      server_id: filters.serverId || undefined,
      enabled: filters.enabled === 'true' ? true : filters.enabled === 'false' ? false : undefined,
    })
    routes.value = res.items
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

function openCreate() {
  editingId.value = ''
  Object.assign(form, { name: '', server_id: '', tool_names: [] })
  dialogVisible.value = true
}

function openEdit(record: Route) {
  editingId.value = record.id
  Object.assign(form, {
    name: record.name,
    server_id: record.server_id,
    tool_names: record.tool_names ?? [],
  })
  dialogVisible.value = true
}

async function submit() {
  if (!form.name.trim() || !form.server_id) {
    message.warning(t('routes.fillRequired'))
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await updateRoute(editingId.value, {
        name: form.name,
        server_id: form.server_id,
        tool_names: form.tool_names,
      })
      message.success(t('routes.updatedOk'))
    } else {
      await createRoute({ ...form, enabled: true })
      message.success(t('routes.createdOk'))
    }
    dialogVisible.value = false
    await load()
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}

async function toggle(record: Route, enabled: boolean) {
  try {
    await toggleRoute(record.id, enabled)
    await load()
  } catch (e) {
    message.error(String(e))
  }
}

async function remove(record: Route) {
  try {
    await deleteRoute(record.id)
    message.success(t('routes.deletedOk'))
    await load()
  } catch (e) {
    message.error(String(e))
  }
}
</script>

<style scoped>
.toolbar {
  margin-bottom: 16px;
}
.filters {
  margin-bottom: 12px;
}
.tool-tag {
  margin-right: 6px;
}
</style>
