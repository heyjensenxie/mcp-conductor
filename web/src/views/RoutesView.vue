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
      <a-table :data-source="routes" :columns="columns" :pagination="false" :row-key="(r: any) => r.id">
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
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { createRoute, deleteRoute, listRoutes, listServers, listTools, toggleRoute, updateRoute } from '@/api'
import type { MCPServer, Route } from '@/types'

const { t } = useI18n()
const routes = ref<Route[]>([])
const servers = ref<MCPServer[]>([])
const allTools = ref<string[]>([])
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

onMounted(load)

async function load() {
  try {
    const [routeList, serverList, toolList] = await Promise.all([listRoutes(), listServers(), listTools()])
    routes.value = routeList
    servers.value = serverList
    allTools.value = toolList.map((tool) => tool.gateway_name)
  } catch (e) {
    message.error(String(e))
  }
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
.tool-tag {
  margin-right: 6px;
}
</style>
