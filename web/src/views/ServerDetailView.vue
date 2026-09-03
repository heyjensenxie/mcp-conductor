<template>
  <a-spin :spinning="loading">
    <a-page-header :title="t('serverDetail.back')" @back="$router.push('/servers')" class="ph">
      <template #subTitle>{{ server?.name }}</template>
      <template #tags>
        <a-tag :color="healthColor(server?.health_status)">{{ server?.health_status }}</a-tag>
      </template>
    </a-page-header>

    <a-descriptions v-if="server" :column="2" bordered size="small" class="mb">
      <a-descriptions-item label="ID">{{ server.id }}</a-descriptions-item>
      <a-descriptions-item :label="t('serverDetail.status')">
        <a-tag :color="server.enabled ? 'green' : 'default'">{{ server.enabled ? t('servers.enabled') : t('servers.disabled') }}</a-tag>
      </a-descriptions-item>
      <a-descriptions-item :label="t('serverDetail.endpoint')">{{ server.endpoint }}</a-descriptions-item>
      <a-descriptions-item :label="t('serverDetail.transport')">{{ server.transport }}</a-descriptions-item>
      <a-descriptions-item :label="t('common.description')" :span="2">{{ server.description || '-' }}</a-descriptions-item>
    </a-descriptions>

    <a-space class="mb">
      <a-button @click="test">{{ t('serverDetail.testConnection') }}</a-button>
      <a-button v-if="server" :danger="server.enabled" @click="toggle">
        {{ server.enabled ? t('serverDetail.disable') : t('serverDetail.enable') }}
      </a-button>
    </a-space>

    <a-tabs v-model:activeKey="tab">
      <a-tab-pane :key="'tools'" :tab="t('serverDetail.tools')">
        <a-table :data-source="tools" :columns="toolColumns" :pagination="false" size="small" :row-key="(r: any) => r.id" />
      </a-tab-pane>
      <a-tab-pane :key="'credentials'" :tab="t('serverDetail.credentials')">
        <div class="toolbar">
          <a-button size="small" type="primary" @click="openCredential">
            <template #icon><PlusOutlined /></template>{{ t('serverDetail.newCredential') }}
          </a-button>
        </div>
        <a-table :data-source="credentials" :columns="credColumns" :pagination="false" size="small" :row-key="(r: any) => r.id">
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'has_value'">
              <a-tag :color="record.has_value ? 'green' : 'default'">
                {{ record.has_value ? t('serverDetail.configured') : t('serverDetail.emptyValue') }}
              </a-tag>
            </template>
          </template>
        </a-table>
      </a-tab-pane>
    </a-tabs>

    <a-modal v-model:open="credVisible" :title="t('serverDetail.newCredential')" :ok-text="t('serverDetail.saveCredential')" :cancel-text="t('common.cancel')" :confirm-loading="credSaving" @ok="saveCredential">
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
          <a-input-password v-model:value="credForm.value" :placeholder="t('serverDetail.credValuePlaceholder')" />
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
import { PlusOutlined } from '@ant-design/icons-vue'
import { createCredential, getServer, listServerCredentials, listServerTools, testServer, toggleServer } from '@/api'
import type { Credential, MCPServer, Tool } from '@/types'

const { t } = useI18n()
const route = useRoute()
const id = computed(() => String(route.params.id))
const server = ref<MCPServer>()
const tools = ref<Tool[]>([])
const credentials = ref<Credential[]>([])
const loading = ref(false)
const tab = ref('tools')

const toolColumns = computed<any[]>(() => [
  { title: t('tools.gatewayName'), key: 'gateway_name', dataIndex: 'gateway_name' },
  { title: t('tools.original'), key: 'original_name', dataIndex: 'original_name', width: 150 },
  { title: t('tools.description'), key: 'description', dataIndex: 'description', ellipsis: true },
])
const credColumns = computed<any[]>(() => [
  { title: t('common.name'), key: 'name', dataIndex: 'name' },
  { title: t('serverDetail.credKind'), key: 'kind', dataIndex: 'kind', width: 140 },
  { title: t('serverDetail.credHeader'), key: 'header', dataIndex: 'header', width: 170 },
  { title: t('serverDetail.valueColumn'), key: 'has_value', width: 100 },
])

onMounted(load)

async function load() {
  loading.value = true
  try {
    server.value = await getServer(id.value)
    tools.value = await listServerTools(id.value)
    credentials.value = await listServerCredentials(id.value)
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
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

async function toggle() {
  if (!server.value) return
  try {
    await toggleServer(id.value, !server.value.enabled)
    await load()
  } catch (e) {
    message.error(String(e))
  }
}

// ---- Credential 表单 ----
const credVisible = ref(false)
const credSaving = ref(false)
const credForm = reactive<{ name: string; kind: 'api_key' | 'static_token'; header: string; value: string }>({
  name: '',
  kind: 'api_key',
  header: '',
  value: '',
})

function openCredential() {
  Object.assign(credForm, { name: '', kind: 'api_key', header: '', value: '' })
  credVisible.value = true
}

async function saveCredential() {
  if (!credForm.name) {
    message.warning(t('serverDetail.credentialNameRequired'))
    return
  }
  credSaving.value = true
  try {
    await createCredential(id.value, { ...credForm })
    message.success(t('serverDetail.credentialSaved'))
    credVisible.value = false
    await load()
  } catch (e) {
    message.error(String(e))
  } finally {
    credSaving.value = false
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
</style>