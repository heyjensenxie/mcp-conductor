<template>
  <div>
    <a-card :bordered="true" class="mb">
      <a-space wrap>
        <a-select v-model:value="serverId" :placeholder="t('testing.selectPlaceholder')" style="width: 320px" @change="onSelectServer">
          <a-select-option v-for="s in servers" :key="s.id" :value="s.id">{{ s.name }} ({{ s.endpoint }})</a-select-option>
        </a-select>
        <a-button type="primary" :disabled="!serverId" :loading="testing" @click="connect">{{ t('testing.testConnection') }}</a-button>
      </a-space>
    </a-card>

    <a-row :gutter="16" v-if="tools.length">
      <a-col :span="12">
        <a-card :bordered="true" :title="t('testing.discoveredTools')">
          <a-menu v-model:selectedKeys="selectedToolKeys" mode="inline" :items="toolMenuItems" style="border-inline-end: none" />
        </a-card>
      </a-col>
      <a-col :span="12">
        <a-card :bordered="true">
          <template #title>
            {{ t('testing.invokeTitle') }}<span v-if="selectedTool"> · {{ selectedTool }}</span>
          </template>
          <template v-if="selectedTool">
            <a-textarea v-model:value="argsText" :rows="6" class="mb" :placeholder="t('testing.argsPlaceholder')" />
            <a-button type="primary" :loading="invoking" @click="invoke">{{ t('testing.invoke') }}</a-button>
            <pre v-if="resultText" class="result">{{ resultText }}</pre>
          </template>
          <a-empty v-else :description="t('testing.resultEmpty')" :image="Empty.PRESENTED_IMAGE_SIMPLE" />
        </a-card>
      </a-col>
    </a-row>
    <a-empty v-else-if="serverId && !loadingTools" :description="t('testing.empty')" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import axios from 'axios'
import { Empty, message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { listServers, listServerTools, testServer } from '@/api'
import type { MCPServer, Tool } from '@/types'

const { t } = useI18n()
const servers = ref<MCPServer[]>([])
const serverId = ref('')
const tools = ref<Tool[]>([])
const selectedToolKeys = ref<string[]>([])
const testing = ref(false)
const invoking = ref(false)
const loadingTools = ref(false)
const argsText = ref('{}')
const resultText = ref('')

const selectedTool = computed(() => selectedToolKeys.value[0] ?? '')

const toolMenuItems = computed(() => {
  const items: any[] = []
  for (const tool of tools.value) {
    items.push({ key: tool.gateway_name, label: tool.gateway_name, title: tool.description })
  }
  return items
})

onMounted(async () => {
  try {
    servers.value = await listServers()
  } catch (e) {
    message.error(String(e))
  }
})

function onSelectServer() {
  tools.value = []
  selectedToolKeys.value = []
  resultText.value = ''
}

async function connect() {
  if (!serverId.value) return
  testing.value = true
  loadingTools.value = true
  try {
    await testServer(serverId.value)
    tools.value = await listServerTools(serverId.value)
    message.success(t('testing.connectedOk'))
  } catch (e) {
    message.error(`${t('testing.connectFail')}: ${e}`)
  } finally {
    testing.value = false
    loadingTools.value = false
  }
}

async function invoke() {
  if (!selectedTool.value) return
  let args: Record<string, unknown> = {}
  try {
    args = JSON.parse(argsText.value || '{}')
  } catch {
    message.warning(t('testing.jsonWarn'))
    return
  }
  invoking.value = true
  resultText.value = ''
  try {
    const resp = await axios.post('/mcp', {
      jsonrpc: '2.0',
      id: Date.now(),
      method: 'tools/call',
      params: { name: selectedTool.value, arguments: args },
    })
    const body = resp.data as any
    if (body.error) resultText.value = `JSON-RPC: ${body.error.message}`
    else if (body.result?.isError)
      resultText.value = (body.result.content || []).map((c: any) => c.text).join('\n')
    else resultText.value = (body.result?.content || []).map((c: any) => c.text).join('\n')
  } catch (e) {
    message.error(`${t('testing.invokeErr')}: ${e}`)
  } finally {
    invoking.value = false
  }
}
</script>

<style scoped>
.mb {
  margin-bottom: 16px;
}
.result {
  margin-top: 12px;
  background: #f5f5f5;
  border-radius: 4px;
  padding: 12px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 320px;
  overflow: auto;
}
</style>