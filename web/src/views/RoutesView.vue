<template>
  <div>
    <div class="toolbar">
      <a-button type="primary" @click="dialogVisible = true">
        <template #icon><PlusOutlined /></template>{{ t('routes.newRoute') }}
      </a-button>
    </div>

    <a-card :bordered="true">
      <a-table :data-source="routes" :columns="columns" :pagination="false" :row-key="(r: any) => r.id">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'tools'">
            <a-tag v-for="tool in record.tool_names || []" :key="tool" class="tool-tag">{{ tool }}</a-tag>
            <span v-if="!(record.tool_names || []).length">-</span>
          </template>
          <template v-else-if="column.key === 'enabled'">
            <a-tag :color="record.enabled ? 'green' : 'default'">{{ record.enabled ? t('common.yes') : t('common.no') }}</a-tag>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal v-model:open="dialogVisible" :title="t('routes.newRoute')" :ok-text="t('routes.create')" :cancel-text="t('common.cancel')" @ok="submit">
      <a-form :label-col="{ span: 6 }" :wrapper-col="{ span: 18 }">
        <a-form-item :label="t('routes.name')" :required="true">
          <a-input v-model:value="form.name" />
        </a-form-item>
        <a-form-item :label="t('routes.serverId')" :required="true">
          <a-input v-model:value="form.server_id" placeholder="srv-1" />
        </a-form-item>
        <a-form-item :label="t('routes.toolNames')">
          <a-input v-model:value="toolNamesText" :placeholder="t('routes.toolNamesPlaceholder')" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { PlusOutlined } from '@ant-design/icons-vue'
import { createRoute, listRoutes } from '@/api'
import type { Route } from '@/types'

const { t } = useI18n()
const routes = ref<Route[]>([])
const dialogVisible = ref(false)
const toolNamesText = ref('')
const form = reactive({ name: '', server_id: '' })

const columns = computed<any[]>(() => [
  { title: t('routes.name'), key: 'name', dataIndex: 'name' },
  { title: t('routes.serverId'), key: 'server_id', dataIndex: 'server_id', width: 150 },
  { title: t('routes.tools'), key: 'tools' },
  { title: t('routes.enabled'), key: 'enabled', dataIndex: 'enabled', width: 100 },
])

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
    message.success(t('routes.createdOk'))
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