<template>
  <a-row :gutter="16" v-if="tools.length">
    <a-col :span="10">
      <a-card :bordered="true" :title="t('testing.discoveredTools')">
        <a-menu v-model:selectedKeys="selectedToolKeys" mode="inline" :items="toolMenuItems" style="border-inline-end: none" />
      </a-card>
    </a-col>
    <a-col :span="14">
      <a-card :bordered="true">
        <template #title>
          {{ t('testing.invokeTitle') }}<span v-if="selectedTool"> · {{ selectedTool }}</span>
        </template>
        <template v-if="selectedTool">
          <a-textarea v-model:value="argsText" :rows="6" class="mb" :placeholder="t('testing.argsPlaceholder')" />
          <a-space class="mb">
            <a-button type="primary" :loading="invoking" @click="invoke">{{ t('testing.invoke') }}</a-button>
            <span v-if="resultMeta" class="meta mono">{{ resultMeta }}</span>
          </a-space>
          <pre v-if="resultText" class="result">{{ resultText }}</pre>
        </template>
        <a-empty v-else :description="t('testing.resultEmpty')" />
      </a-card>
    </a-col>
  </a-row>
  <a-empty v-else :description="t('testing.empty')" />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import axios from 'axios'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import type { Tool } from '@/types'

const { t } = useI18n()
const props = defineProps<{ tools: Tool[] }>()

const selectedToolKeys = ref<string[]>([])
const invoking = ref(false)
const argsText = ref('{}')
const resultText = ref('')
const resultMeta = ref('')

const selectedTool = computed(() => selectedToolKeys.value[0] ?? '')

const toolMenuItems = computed(() => {
  const items: any[] = []
  for (const tool of props.tools) {
    items.push({ key: tool.gateway_name, label: tool.gateway_name, title: tool.description })
  }
  return items
})

// 每次 tools 变化后默认选中第一个，便于快速试调。
watch(
  () => props.tools,
  (list) => {
    selectedToolKeys.value = list.length ? [list[0].gateway_name] : []
    argsText.value = '{}'
    resultText.value = ''
    resultMeta.value = ''
  },
  { immediate: true },
)

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
  resultMeta.value = ''
  const start = performance.now()
  try {
    const resp = await axios.post('/mcp', {
      jsonrpc: '2.0',
      id: Date.now(),
      method: 'tools/call',
      params: { name: selectedTool.value, arguments: args },
    })
    const latency = Math.round(performance.now() - start)
    const body = resp.data as any
    let text: string
    if (body.error) {
      text = `JSON-RPC: ${body.error.message}`
      resultMeta.value = `${t('traffic.latencyMs')}: ${latency}ms`
    } else if (body.result?.isError) {
      text = (body.result.content || []).map((c: any) => c.text).join('\n')
      resultMeta.value = `${t('traffic.status')}: error · ${t('traffic.latencyMs')}: ${latency}ms`
    } else {
      text = (body.result?.content || []).map((c: any) => c.text).join('\n')
      resultMeta.value = `${t('traffic.latencyMs')}: ${latency}ms`
    }
    resultText.value = text
  } catch (e) {
    resultMeta.value = ''
    message.error(`${t('testing.invokeErr')}: ${e}`)
  } finally {
    invoking.value = false
  }
}
</script>

<style scoped>
.mb {
  margin-bottom: 12px;
}
.meta {
  font-size: 12px;
  color: #888;
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
