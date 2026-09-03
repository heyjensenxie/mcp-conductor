<template>
  <div>
    <div class="toolbar">
      <a-button type="primary" @click="dialogVisible = true">
        <template #icon><PlusOutlined /></template>New Route
      </a-button>
    </div>

    <a-card :bordered="true">
      <a-table :data-source="routes" :columns="columns" :pagination="false" :row-key="(r: any) => r.id">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'tools'">
            <a-tag v-for="t in record.tool_names || []" :key="t" class="tool-tag">{{ t }}</a-tag>
            <span v-if="!(record.tool_names || []).length">-</span>
          </template>
          <template v-else-if="column.key === 'enabled'">
            <a-tag :color="record.enabled ? 'green' : 'default'">{{ record.enabled ? 'yes' : 'no' }}</a-tag>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal v-model:open="dialogVisible" title="New Route" ok-text="Create" cancel-text="Cancel" @ok="submit">
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item label="Name" :required="true">
          <a-input v-model:value="form.name" />
        </a-form-item>
        <a-form-item label="Server ID" :required="true">
          <a-input v-model:value="form.server_id" placeholder="如 srv-1" />
        </a-form-item>
        <a-form-item label="Tool Names">
          <a-input v-model:value="toolNamesText" placeholder="逗号分隔的对外工具名，可留空" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { PlusOutlined } from '@ant-design/icons-vue'
import { createRoute, listRoutes } from '@/api'
import type { Route } from '@/types'

const routes = ref<Route[]>([])
const dialogVisible = ref(false)
const toolNamesText = ref('')
const form = reactive({ name: '', server_id: '' })

const columns: any[] = [
  { title: 'Name', key: 'name', dataIndex: 'name' },
  { title: 'Server', key: 'server_id', dataIndex: 'server_id', width: 150 },
  { title: 'Tools', key: 'tools' },
  { title: 'Enabled', key: 'enabled', dataIndex: 'enabled', width: 100 },
]

onMounted(load)

async function load() {
  try {
    routes.value = await listRoutes()
  } catch (e) {
    message.error(String(e))
  }
}

async function submit() {
  try {
    const toolNames = toolNamesText.value
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    await createRoute({ ...form, tool_names: toolNames, enabled: true })
    message.success('已创建')
    dialogVisible.value = false
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