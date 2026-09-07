<template>
  <a-spin :spinning="loading">
    <template v-if="tool">
      <a-page-header :title="t('toolDetail.back')" @back="$router.push('/tools')" class="ph">
        <template #subTitle>
          <a-space :size="8">
            <a-typography-text code>{{ tool.gateway_name }}</a-typography-text>
            <a-tag v-if="tool.name_overridden" color="blue">{{ t('tools.customized') }}</a-tag>
            <a-tag :color="tool.enabled ? 'green' : 'default'">
              {{ tool.enabled ? t('toolDetail.enabled') : t('toolDetail.disabled') }}
            </a-tag>
          </a-space>
        </template>
        <template #extra>
          <a-button size="small" @click="openEdit">
            <template #icon><EditOutlined /></template>{{ t('common.edit') }}
          </a-button>
        </template>
      </a-page-header>

      <!-- 基本信息 -->
      <a-card :bordered="true" class="mb" :title="t('common.description')">
        <a-descriptions :column="2" bordered size="small">
          <a-descriptions-item :label="t('toolDetail.gateway')">
            <a-typography-text code>{{ tool.gateway_name }}</a-typography-text>
          </a-descriptions-item>
          <a-descriptions-item :label="t('toolDetail.original')">{{ tool.original_name }}</a-descriptions-item>
          <a-descriptions-item :label="t('toolDetail.server')">
            <router-link v-if="serverName" :to="`/servers/${tool.server_id}`" class="link">{{ serverName }}</router-link>
            <span v-else class="mono">{{ tool.server_id }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('toolDetail.updated')">{{ tool.updated_at }}</a-descriptions-item>
          <a-descriptions-item :label="t('toolDetail.description')" :span="2">{{ tool.description || '-' }}</a-descriptions-item>
        </a-descriptions>
        <a-alert
          v-if="tool.description_overridden || tool.input_schema_overridden || tool.name_overridden"
          type="info"
          show-icon
          class="mt"
          :message="t('toolDetail.customizedNote')"
        />
      </a-card>

      <!-- Schema：默认表格，右上角可切 JSON 原始定义 -->
      <InputSchemaView class="mb" :title="t('toolDetail.schema')" :schema="tool.input_schema" />
      <InputSchemaView
        v-if="tool.input_schema_overridden"
        class="mb"
        :title="t('toolDetail.sourceSchema')"
        :schema="tool.source_input_schema"
      />

      <!-- 手动调用 -->
      <a-card :bordered="true">
        <template #title>{{ t('toolDetail.invokeTitle') }}</template>
        <a-alert type="info" show-icon class="mb" :message="t('toolDetail.invokeNote')" />

        <a-radio-group v-model:value="argMode" class="mb" button-style="solid" size="small">
          <a-radio-button value="form">{{ t('toolDetail.argsAuto') }}</a-radio-button>
          <a-radio-button value="json">{{ t('toolDetail.argsJson') }}</a-radio-button>
        </a-radio-group>

        <div v-if="argMode === 'form'" class="fields">
          <ParamEditor v-if="rootNodes.length" :fields="rootNodes" :model="rootModel" />
          <div v-else class="muted">{{ t('toolDetail.argsAuto') }} — <span class="mono">{}</span></div>
        </div>
        <a-textarea v-else v-model:value="jsonArgs" :rows="6" class="mb mono" :placeholder='{ "q": "x" }' />

        <a-space class="mb">
          <a-button type="primary" :loading="invoking" @click="invoke">
            <template #icon><PlayCircleOutlined /></template>{{ t('toolDetail.invoke') }}
          </a-button>
          <a-button @click="resetArgs">{{ t('toolDetail.reset') }}</a-button>
          <span v-if="resultMeta" class="meta mono">{{ resultMeta }}</span>
        </a-space>

        <template v-if="resultText !== '' || resultMeta">
          <a-divider orientation="left">{{ t('toolDetail.result') }}</a-divider>
          <pre :class="['result', resultError ? 'result-error' : '']" class="mono">{{ resultText || t('toolDetail.contentEmpty') }}</pre>
        </template>
      </a-card>

      <!-- 编辑 / 恢复源定义（与 Tools 列表页弹窗同语义：updateTool + 逐字段 reset） -->
      <a-modal v-model:open="editVisible" :title="t('tools.editTitle')" :confirm-loading="saving" :width="760" @ok="save">
        <a-form layout="vertical">
          <a-form-item :label="t('tools.original')"><a-input :value="tool.original_name" disabled /></a-form-item>
          <a-form-item :label="t('tools.gatewayName')" :help="t('tools.renameHint')">
            <a-input v-model:value="form.gatewayName" />
            <a-button v-if="tool.name_overridden" type="link" size="small" class="reset" @click="form.gatewayName = canonicalName; resetName = true">{{ t('tools.resetSource') }}</a-button>
          </a-form-item>
          <a-form-item :label="t('tools.description')">
            <a-textarea v-model:value="form.description" :rows="3" />
            <a-button v-if="tool.description_overridden" type="link" size="small" class="reset" @click="form.description = tool.source_description ?? ''; resetDescription = true">{{ t('tools.resetSource') }}</a-button>
          </a-form-item>
          <a-form-item :label="t('tools.inputSchema')" :help="t('tools.schemaHint')">
            <InputSchemaEditor ref="schemaEditor" v-model="form.inputSchema" :seed="editSeed" />
            <a-button v-if="tool.input_schema_overridden" type="link" size="small" class="reset" @click="resetSchemaToSource">{{ t('tools.resetSource') }}</a-button>
          </a-form-item>
        </a-form>
      </a-modal>
    </template>
    <a-empty v-else-if="!loading" :description="t('toolDetail.notFound')">
      <a-button type="primary" @click="$router.push('/tools')">{{ t('toolDetail.back') }}</a-button>
    </a-empty>
  </a-spin>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import axios from 'axios'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { PlayCircleOutlined, EditOutlined } from '@ant-design/icons-vue'
import { getTool, listAllServers, updateTool } from '@/api'
import InputSchemaView from '@/components/InputSchemaView.vue'
import InputSchemaEditor from '@/components/InputSchemaEditor.vue'
import ParamEditor from '@/components/ParamEditor.vue'
import { schemaToNodes, objectSkeleton, collectNodes, jsonScaffold } from '@/utils/schema'
import type { ParamNode } from '@/utils/schema'
import type { MCPServer, Tool } from '@/types'

const { t } = useI18n()
const route = useRoute()
const id = computed(() => String(route.params.id))

const tool = ref<Tool>()
const servers = ref<MCPServer[]>([])
const loading = ref(false)

// —— 手动调用：按 Schema 生成的树形参数表单 ——
// 结构语义与详情页只读表格同源（@/utils/schema）；编辑写入同一棵 reactive 模型，
// 提交时按节点形态逐字段读取并校验，标量/枚举、嵌套对象、数组、JSON 回退各走对应控件。
const rootNodes = ref<ParamNode[]>([])
const rootModel = ref<Record<string, any>>({})
const argMode = ref<'form' | 'json'>('form')
const jsonArgs = ref('{}')
// JSON 参数骨架只首次注入一次，用户后续手改的内容不被覆盖。
const jsonInjected = ref(false)

const invoking = ref(false)
const resultText = ref('')
const resultMeta = ref('')
const resultError = ref(false)

// —— 编辑 / 恢复源定义（复用 Tools 列表页弹窗语义）——
const editVisible = ref(false)
const saving = ref(false)
const schemaEditor = ref<InstanceType<typeof InputSchemaEditor> | null>(null)
const editSeed = ref(0)
const resetName = ref(false)
const resetDescription = ref(false)
const resetInputSchema = ref(false)
const form = reactive({ gatewayName: '', description: '', inputSchema: '' })
// canonicalName：按 Server 命名空间 + 上游原名复原对外名（reset 用）。
const canonicalName = computed(() => {
  const namespace = serverName.value.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '') || 'server'
  return `${namespace}.${tool.value?.original_name ?? ''}`
})

const serverName = computed(() => servers.value.find((s) => s.id === tool.value?.server_id)?.name ?? '')

onMounted(async () => {
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
    tool.value = await getTool(id.value)
    buildFields()
  } catch {
    tool.value = undefined
  } finally {
    loading.value = false
  }
}

function buildFields() {
  const spec = tool.value
  if (!spec) return
  rootNodes.value = schemaToNodes((spec.input_schema ?? null) as Record<string, unknown> | null)
  rootModel.value = reactive(objectSkeleton(rootNodes.value))
  jsonArgs.value = '{}'
  jsonInjected.value = false
}

function resetArgs() {
  buildFields()
  if (argMode.value === 'json') {
    jsonArgs.value = jsonScaffold(rootNodes.value)
    jsonInjected.value = true
  }
  resultText.value = ''
  resultMeta.value = ''
  resultError.value = false
}

// —— 以 JSON 指定参数：JSON 骨架由 @/utils/schema 的 jsonScaffold 统一生成 ——
// 首次切到 JSON 模式时注入骨架；已注入/用户手改后不再覆盖。
function ensureJsonScaffold() {
  if (!jsonInjected.value) {
    jsonArgs.value = jsonScaffold(rootNodes.value)
    jsonInjected.value = true
  }
}
watch(argMode, (m) => {
  if (m === 'json') ensureJsonScaffold()
})

// ---- 参数收集（必填 / JSON 校验）由 @/utils/schema 的 collectNodes 统一提供 ----

// 收集并校验参数；非法时提示并返回 null。
function collectArgs(): Record<string, unknown> | null {
  if (!tool.value) return null
  if (argMode.value === 'json') {
    try {
      const parsed = JSON.parse(jsonArgs.value || '{}')
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('obj')
      return parsed as Record<string, unknown>
    } catch {
      message.warning(t('toolDetail.jsonInvalid'))
      return null
    }
  }
  const res = collectNodes(rootNodes.value, rootModel.value, {
    required: t('toolDetail.required'),
    jsonInvalid: t('toolDetail.jsonInvalid'),
  })
  if ('error' in res) {
    message.warning(res.error)
    return null
  }
  return res.args
}

async function invoke() {
  if (!tool.value) return
  const args = collectArgs()
  if (args === null) return
  if (!tool.value.enabled) {
    message.warning(t('toolDetail.disabledTool'))
    return
  }
  invoking.value = true
  resultText.value = ''
  resultMeta.value = ''
  resultError.value = false
  const start = performance.now()
  try {
    const resp = await axios.post(
      '/mcp',
      {
        jsonrpc: '2.0',
        id: Date.now(),
        method: 'tools/call',
        params: { name: tool.value.gateway_name, arguments: args },
      },
      { headers: { 'X-Api-Key': localStorage.getItem('mc_token') ?? '' } },
    )
    const latency = Math.round(performance.now() - start)
    const body = resp.data as any
    if (body.error) {
      resultError.value = true
      resultText.value = `JSON-RPC ${body.error.code ?? ''}: ${body.error.message}`
      resultMeta.value = `${t('toolDetail.errResult')} · ${t('toolDetail.latency')}: ${latency}ms`
    } else {
      const isError = Boolean(body.result?.isError)
      const content = (body.result?.content ?? []).map((c: any) => c.text).join('\n')
      resultText.value = isError ? content || t('toolDetail.contentEmpty') : content || t('toolDetail.contentEmpty')
      resultError.value = isError
      resultMeta.value = `${isError ? t('toolDetail.errResult') : t('toolDetail.okResult')} · ${t('toolDetail.latency')}: ${latency}ms`
    }
  } catch (e) {
    resultError.value = true
    resultMeta.value = t('toolDetail.errResult')
    resultText.value = String(e)
  } finally {
    invoking.value = false
  }
}

// —— 编辑 / 恢复源定义 ——
function openEdit() {
  const spec = tool.value
  if (!spec) return
  form.gatewayName = spec.gateway_name
  form.description = spec.description ?? ''
  form.inputSchema = JSON.stringify(spec.input_schema ?? {}, null, 2)
  resetName.value = resetDescription.value = resetInputSchema.value = false
  editSeed.value += 1
  editVisible.value = true
}

function resetSchemaToSource() {
  resetInputSchema.value = true
  form.inputSchema = JSON.stringify(tool.value?.source_input_schema ?? {}, null, 2)
  // 即使目标串与当前已提交串相同，也强制编辑器按上游重建，丢弃未提交的编辑态。
  editSeed.value += 1
}

async function save() {
  if (!tool.value) return
  // 由编辑器统一提交：表格模式会校验并重建，JSON 模式做语法校验；非法时返回 null。
  let schemaText = form.inputSchema
  if (schemaEditor.value) {
    const committed = schemaEditor.value.commit()
    if (committed === null) return
    schemaText = committed
  }
  let schema: Record<string, unknown>
  try {
    schema = JSON.parse(schemaText)
  } catch {
    message.warning(t('tools.invalidSchema'))
    return
  }
  saving.value = true
  try {
    await updateTool(tool.value.id, {
      gateway_name: form.gatewayName,
      description: form.description,
      input_schema: schema,
      reset_name: resetName.value,
      reset_description: resetDescription.value,
      reset_input_schema: resetInputSchema.value,
    })
    editVisible.value = false
    message.success(t('tools.updatedOk'))
    await load()
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.ph {
  padding: 0 0 4px;
}
.mb {
  margin-bottom: 16px;
}
.mt {
  margin-top: 12px;
}
.link {
  color: #1677ff;
}
.fields {
  margin-bottom: 12px;
}
.meta {
  font-size: 12px;
  color: var(--mc-ink-3);
}
.result {
  background: #f5f5f5;
  border-radius: 4px;
  padding: 12px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 360px;
  overflow: auto;
  margin-bottom: 0;
}
.result-error {
  background: #fff1f0;
  color: #cf1322;
}
.muted {
  color: var(--mc-ink-3);
  font-size: 12px;
}
.reset {
  padding-left: 0;
  font-size: 12px;
  height: 22px;
}
</style>
