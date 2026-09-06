<template>
  <a-spin :spinning="loading" class="akd-root">
    <div class="akd">
      <!-- ============ 页面 Header ============ -->
      <header class="ph">
        <div class="ph-main">
          <div class="ph-meta">
            <a-button type="text" size="small" class="ph-back" @click="router.push('/access')">
              <template #icon><ArrowLeftOutlined /></template>{{ t('common.back') }}
            </a-button>
            <span class="ph-eyebrow">ACCESS KEY</span>
            <span class="ph-eyebrow-var mono">{{ key?.subject ?? '…' }}</span>
          </div>
          <div class="ph-titleline">
            <h1 class="ph-name">{{ key?.name ?? '…' }}</h1>
            <p class="ph-sub">{{ t('access.detailSubtitle') }}</p>
          </div>
        </div>
        <div class="ph-side">
          <div class="ph-states">
            <span class="state-chip" :class="{ off: !key?.enabled }">
              <i class="state-dot" :class="key?.enabled ? 'ok' : 'off'" />
              {{ key?.enabled ? t('access.active') : t('access.inactive') }}
            </span>
            <span v-if="dirty" class="state-chip dirty">
              <i class="state-dot warn" />{{ t('access.unsaved') }}
            </span>
          </div>
          <div class="ph-btns">
            <a-button :loading="invokeRunning" @click="openInvoke">
              <template #icon><ExperimentOutlined /></template>{{ t('access.invokeAsKey') }}
            </a-button>
            <a-button :loading="saving" :disabled="saving" @click="save">{{ t('common.save') }}</a-button>
            <a-button type="primary" :loading="saving" :disabled="saving" @click="save">{{ t('access.publishGrants') }}</a-button>
          </div>
        </div>
      </header>

      <!-- 密钥提示（不回读，可重置） -->
      <section v-if="key" class="secret-note">
        <span class="sn-ic"><InfoCircleOutlined /></span>
        <div class="sn-tx">
          <p class="sn-title">{{ t('access.secretNotReadableTitle') }}</p>
          <p class="sn-body">{{ t('access.secretNoteIntro') }} {{ t('access.secretNoteLost') }}</p>
          <p class="sn-body sn-code">
            <code>Authorization: Bearer &lt;secret&gt;</code>
            <span class="sn-alt">{{ t('access.secretNoteAlt') }}</span>
          </p>
        </div>
        <a-button size="small" class="sn-rotate" :loading="rotating" @click="rotateVisible = true">
          <template #icon><ReloadOutlined /></template>{{ t('access.rotateBtn') }}
        </a-button>
      </section>

      <!-- ============ Key 基础策略 ============ -->
      <section v-if="key" class="base">
        <h2 class="base-title">{{ t('access.keyBasics') }}</h2>
        <div class="bf">
          <span class="bf-label">SUBJECT</span>
          <span class="bf-value mono bf-subject">{{ key.subject }}</span>
        </div>
        <div class="bf">
          <span class="bf-label">QPS</span>
          <a-input-number v-model:value="form.qps" :min="0" :precision="0" :controls="false" size="small" class="bf-num" />
        </div>
        <div class="bf">
          <span class="bf-label">BURST</span>
          <a-input-number v-model:value="form.burst" :min="0" :precision="0" :controls="false" size="small" class="bf-num" />
        </div>
        <div class="bf">
          <span class="bf-label">STATUS</span>
          <span class="bf-toggle">
            <a-switch v-model:checked="form.enabled" size="small" />
            <span class="bf-toggle-txt" :class="{ off: !form.enabled }">{{ form.enabled ? t('access.active') : t('access.inactive') }}</span>
          </span>
        </div>
      </section>

      <!-- ============ 三栏工作区 ============ -->
      <section v-if="key" class="work">
        <!-- 左：平台工具目录 -->
        <aside class="pane pane--catalog">
          <header class="pane-head">
            <div class="pane-head-txt">
              <p class="pane-kicker">PLATFORM CATALOG</p>
              <h3 class="pane-title">{{ t('access.catalogTitle') }}</h3>
            </div>
            <span class="pane-count mono">{{ enabledTools.length }}</span>
          </header>

          <div class="cat-tools">
            <a-input
              v-model:value="query"
              allow-clear
              size="small"
              class="cat-search"
              :placeholder="t('access.searchTools')"
            >
              <template #prefix><SearchOutlined /></template>
            </a-input>

            <div class="cat-scroll">
              <template v-if="servers.length === 0">
                <p class="cat-note">{{ t('access.noServersHint') }}</p>
              </template>

              <section v-for="group in catalogGroups" :key="group.server.id" class="sg">
                <header class="sg-head">
                  <i class="sdot" :class="serverDotClass(group.server)" />
                  <b class="sg-name">{{ group.server.name }}</b>
                  <span class="sg-count mono">{{ group.tools.length }}</span>
                </header>
                <button
                  v-for="tool in group.tools"
                  :key="tool.id"
                  type="button"
                  class="ctool"
                  :class="{ granted: isGranted(tool.gateway_name), cur: focusTool === tool.gateway_name }"
                  @mouseenter="hover = tool.gateway_name"
                  @mouseleave="hover = ''"
                  @click="toggleTool(tool)"
                >
                  <span class="ctool-ck">{{ isGranted(tool.gateway_name) ? '✓' : '+' }}</span>
                  <span class="ctool-tx">
                    <b class="mono">{{ tool.gateway_name }}</b>
                    <i>{{ tool.description || tool.original_name }}</i>
                  </span>
                </button>
              </section>

              <p v-if="servers.length > 0 && catalogGroups.length === 0" class="cat-note">{{ t('common.empty') }}</p>
            </div>

            <p class="cat-foot">{{ t('access.exposedToolsHint') }}</p>
          </div>
        </aside>

        <!-- 中：授权拓扑（视觉中心） -->
        <main class="pane pane--graph">
          <header class="pane-head">
            <div class="pane-head-txt">
              <p class="pane-kicker">AUTHORIZATION GRAPH</p>
              <h3 class="pane-title">{{ t('access.authTopology') }}</h3>
            </div>
            <span class="pane-count mono">{{ grants.length }}</span>
          </header>

          <div class="canvas">
            <div class="topo">
              <!-- Access Key -->
              <article class="g-node g-node--key" :class="{ hot: grants.length > 0 }">
                <p class="g-eyebrow">ACCESS KEY</p>
                <h4 class="g-name">{{ key.name }}</h4>
                <p class="g-sub mono">{{ key.subject }}</p>
                <div class="g-chips">
                  <span class="g-chip acc"><b class="mono">{{ visibleGrantCount }}</b>{{ t('access.grantsUnit') }}</span>
                  <span class="g-chip" :class="key.enabled ? 'on' : ''">{{ key.enabled ? t('access.active') : t('access.inactive') }}</span>
                </div>
              </article>

              <!-- 连接：Key → Allowlist -->
              <span class="g-arrow-cell g-arrow-cell--span" :class="{ hot: grants.length > 0 }"><i class="g-arrow" /></span>

              <!-- Gateway Allowlist -->
              <article class="g-node g-node--allowlist" :class="{ hot: grants.length > 0 }">
                <p class="g-eyebrow">GATEWAY ALLOWLIST</p>
                <h4 class="g-name">{{ t('access.allowlist') }}</h4>
                <p class="g-desc">{{ t('access.allowlistDesc') }}</p>
                <div class="g-chips">
                  <span class="g-chip"><b class="mono">{{ grants.length }}</b>{{ t('access.rulesText') }}</span>
                  <span class="g-chip mode">{{ hasWildcard ? t('access.modeAll') : t('access.modeAllow') }}</span>
                </div>
              </article>

              <!-- 每个 Server 一行：箭头 + Server 节点 -->
              <template v-for="(row, idx) in serverRows" :key="idx">
                <span
                  v-if="row.server"
                  class="g-arrow-cell g-arrow-cell--row"
                  :class="{ hot: rowHot(row) }"
                ><i class="g-arrow" /></span>

                <div class="g-slot">
                  <article
                    v-if="row.server"
                    class="g-node g-node--server"
                    :class="{ hot: rowHot(row) }"
                  >
                    <header class="gs-head">
                      <i class="sdot" :class="serverDotClass(row.server)" />
                      <div class="gs-txt">
                        <b class="gs-name">{{ row.server.name }}</b>
                        <span class="gs-health" :class="serverDotClass(row.server)">{{ healthLabel(row.server) }}</span>
                      </div>
                      <span class="gs-count mono">{{ row.tools.length }}</span>
                    </header>
                    <ul class="g-tools">
                      <li
                        v-for="tool in row.tools"
                        :key="tool.id"
                        class="g-tool"
                        :class="{ cur: focusTool === tool.gateway_name }"
                        :title="tool.gateway_name"
                        @mouseenter="hover = tool.gateway_name"
                        @mouseleave="hover = ''"
                        @click="focusAndOpen(tool.gateway_name)"
                      >
                        <span class="g-tool-ck">✓</span>
                        <span class="mono">{{ tool.gateway_name }}</span>
                      </li>
                    </ul>
                  </article>

                  <div v-else class="g-empty">
                    <p class="g-empty-title">{{ t('access.noGranted') }}</p>
                    <p class="g-empty-hint">{{ t('access.addGrantTool') }}</p>
                  </div>
                </div>
              </template>
            </div>
          </div>

          <footer class="graph-foot">{{ t('access.canvasDetailHint') }}</footer>
        </main>

        <!-- 右：授权配置 -->
        <aside class="pane pane--cfg">
          <header class="pane-head">
            <div class="pane-head-txt">
              <p class="pane-kicker">GRANT CONFIGURATION</p>
              <h3 class="pane-title">{{ t('access.grantPanelTitle') }}</h3>
            </div>
            <span class="pane-count mono">{{ grants.length }} {{ t('access.grantsUnit') }}</span>
          </header>

          <div class="cfg-scroll">
            <p v-if="grantRows.length === 0" class="cat-note cfg-empty">{{ t('access.noGranted') }}</p>

            <article
              v-for="g in grantRows"
              :key="g.gw"
              class="gc"
              :class="{ cur: focusTool === g.gw, open: !!openGws[g.gw] }"
              @mouseenter="hover = g.gw"
              @mouseleave="hover = ''"
            >
              <div class="gc-head" @click="toggleCard(g.gw)">
                <span class="gc-tag" :class="g.kind">{{ tagLabel(g.kind) }}</span>
                <b class="gc-name mono">{{ g.gw }}</b>
                <span class="gc-spacer" />
                <button type="button" class="icon-btn del" :title="t('common.delete')" @click.stop="removeGrant(g)">
                  <DeleteOutlined />
                </button>
                <span class="gc-chev" :class="{ open: !!openGws[g.gw] }"><DownOutlined /></span>
              </div>

              <p v-if="g.kind === 'tool' && !openGws[g.gw]" class="gc-src">
                <i class="sdot idle" />{{ g.serverName || '—' }}
              </p>

              <transition name="grow">
                <div v-show="openGws[g.gw]" class="gc-body">
                  <div v-if="g.kind === 'tool' && g.serverName" class="gc-src gc-src--open">
                    <i class="sdot idle" />{{ g.serverName }}
                  </div>

                  <!-- 请求头约束 -->
                  <div class="gc-blk">
                    <p class="gc-blk-title">
                      {{ t('access.headerRule') }}
                      <b class="mono">{{ g.headers.length }}</b>
                    </p>
                    <div v-for="(h, hi) in g.headers" :key="hi" class="pair">
                      <a-input v-model:value="h.k" size="small" :placeholder="t('access.headerKey')" />
                      <a-input v-model:value="h.v" size="small" :placeholder="t('access.headerValue')" />
                      <button type="button" class="icon-btn x" @click="g.headers.splice(hi, 1)">×</button>
                    </div>
                    <a-button type="text" size="small" class="blk-add" @click="g.headers.push({ k: '', v: '' })">
                      <template #icon><PlusOutlined /></template>{{ t('access.addHeaderConstraint') }}
                    </a-button>
                  </div>

                  <!-- 固定参数 -->
                  <div class="gc-blk">
                    <p class="gc-blk-title">
                      {{ t('access.paramRule') }}
                      <b class="mono">{{ g.defargs.length }}</b>
                    </p>
                    <div v-for="(arg, ai) in g.defargs" :key="ai" class="pair">
                      <a-input v-model:value="arg.k" size="small" :placeholder="t('access.argKey')" />
                      <a-input v-model:value="arg.v" size="small" :placeholder="t('access.argValue')" />
                      <button type="button" class="icon-btn x" @click="g.defargs.splice(ai, 1)">×</button>
                    </div>
                    <a-button type="text" size="small" class="blk-add" @click="g.defargs.push({ k: '', v: '' })">
                      <template #icon><PlusOutlined /></template>{{ t('access.addParamConstraint') }}
                    </a-button>
                  </div>
                </div>
              </transition>
            </article>
          </div>
        </aside>
      </section>

      <!-- 重置密钥：确认 -->
      <a-modal
        v-model:open="rotateVisible"
        :title="t('access.rotateTitle')"
        :ok-text="t('access.rotateOk')"
        :cancel-text="t('common.cancel')"
        :ok-button-props="{ danger: true }"
        :confirm-loading="rotating"
        width="460px"
        @ok="doRotateKey"
      >
        <p class="rotate-desc">{{ t('access.rotateDesc') }}</p>
      </a-modal>

      <!-- 重置密钥：新明文仅显示一次 -->
      <a-modal v-model:open="secretVisible" :title="t('access.rotateDoneTitle')" :footer="null" width="560px">
        <a-alert type="warning" show-icon class="mb" :message="t('access.secretOnce')" />
        <a-input-group compact class="secret-row">
          <a-input :value="newSecret" read-only class="mono" />
          <a-button @click="copyNewSecret"><template #icon><CopyOutlined /></template>{{ t('access.copy') }}</a-button>
        </a-input-group>
      </a-modal>

      <!-- 试调用：以该 Key 身份端到端调用一次（Operator 触发，后台免明文，不写遥测） -->
      <a-modal
        v-model:open="invokeVisible"
        :title="t('access.invokeTitle')"
        :footer="null"
        width="640px"
      >
        <p class="inv-desc">{{ t('access.invokeDesc') }}</p>
        <a-form layout="vertical">
          <a-form-item :label="t('access.invokeGatewayTool')">
            <a-select
              v-model:value="invokeTool"
              show-search
              option-filter-prop="value"
              allow-clear
              :placeholder="t('access.invokeToolPlaceholder')"
            >
              <a-select-option v-for="g in enabledTools" :key="g.gateway_name" :value="g.gateway_name">{{ g.gateway_name }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item :label="t('access.invokeArgs')">
            <a-textarea v-model:value="invokeArgsText" :rows="4" class="mono" placeholder='{ "q": "…" }' />
          </a-form-item>
          <div class="inv-actions">
            <a-button type="primary" :loading="invokeRunning" :disabled="!invokeTool" @click="runInvoke">
              <template #icon><PlayCircleOutlined /></template>{{ t('access.invokeRun') }}
            </a-button>
          </div>
        </a-form>

        <template v-if="invokeOut">
          <a-alert v-if="invokeOut.type === 'denied'" type="error" show-icon class="inv-result" :message="invokeOut.message" />
          <div v-else class="inv-result">
            <a-alert v-if="invokeOut.res.is_error" type="warning" show-icon class="mb"
              :message="`${invokeOut.res.error_code} — ${invokeOut.res.message || ''}`" />
            <div class="inv-meta mono">
              <template v-if="invokeOut.res.server_id">{{ t('access.invokeServer') }} {{ invokeOut.res.server_id }} · {{ t('access.invokeInstance') }} {{ invokeOut.res.instance_id }}</template>
              <template v-else>-</template>
              · {{ t('access.invokeLatency') }} {{ invokeOut.res.latency_ms }}ms
            </div>
            <pre v-if="invokeOut.res.content" class="inv-content">{{ invokeOut.res.content }}</pre>
            <p v-else-if="!invokeOut.res.is_error" class="muted">{{ t('access.invokeNoContent') }}</p>
          </div>
        </template>
      </a-modal>
    </div>
  </a-spin>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { ArrowLeftOutlined, CopyOutlined, DeleteOutlined, DownOutlined, ExperimentOutlined, InfoCircleOutlined, PlayCircleOutlined, PlusOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons-vue'
import { useI18n } from 'vue-i18n'
import { getKey, invokeKey, listAllServers, listAllTools, rotateKey, updateKey } from '@/api'
import type { AccessKey, KeyInvokeResult, MCPServer, Tool, ToolGrant } from '@/types'

interface GrantEditor { gw: string; headers: { k: string; v: string }[]; defargs: { k: string; v: string }[] }
type GrantKind = 'tool' | 'global' | 'pattern'
interface GrantRow extends GrantEditor { kind: GrantKind; serverName?: string }
interface ServerRow { server: MCPServer | null; tools: Tool[] }

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const saving = ref(false)
const key = ref<AccessKey | null>(null)
const tools = ref<Tool[]>([])
const servers = ref<MCPServer[]>([])

const query = ref('')
const focusTool = ref('')
const hover = ref('')
const openGws = reactive<Record<string, boolean>>({})
const rotateVisible = ref(false)
const rotating = ref(false)
const newSecret = ref('')
const secretVisible = ref(false)

// ---- 试调用：以该 Key 身份端到端调用（Operator 触发，后台免明文，不写遥测）----
const invokeVisible = ref(false)
// 未选保持 undefined：antd Select 仅在值为空(null/undefined)时展示占位文案。
const invokeTool = ref<string | undefined>(undefined)
const invokeArgsText = ref('')
const invokeRunning = ref(false)
const invokeOut = ref<{ type: 'ok'; res: KeyInvokeResult } | { type: 'denied'; message: string } | null>(null)

const grants = ref<GrantEditor[]>([])
const form = reactive({ qps: 0, burst: 0, enabled: true })
let baseline = ''

// ---- 派生数据 ----
const enabledTools = computed(() => tools.value.filter((tool) => tool.enabled))

const toolByName = computed(() => new Map<string, Tool>(tools.value.map((tool) => [tool.gateway_name, tool])))
const toolsByServer = computed(() => {
  const map = new Map<string, Tool[]>()
  for (const tool of tools.value) {
    const arr = map.get(tool.server_id) ?? []
    arr.push(tool)
    map.set(tool.server_id, arr)
  }
  return map
})

const catalogGroups = computed(() =>
  servers.value
    .map((server) => ({
      server,
      tools: enabledTools.value.filter(
        (tool) =>
          tool.server_id === server.id &&
          `${tool.gateway_name} ${tool.description ?? ''} ${tool.original_name}`
            .toLowerCase()
            .includes(query.value.toLowerCase()),
      ),
    }))
    .filter((group) => group.tools.length > 0),
)

const visibleGrantCount = computed(() => grants.value.filter((grant) => !grant.gw.includes('*')).length)
const hasWildcard = computed(() => grants.value.some((grant) => grant.gw.includes('*')))

function kindOf(gw: string): GrantKind {
  if (gw === '*') return 'global'
  if (gw.includes('*')) return 'pattern'
  return 'tool'
}

const grantRows = computed<GrantRow[]>(() => {
  const rank = { global: 0, pattern: 1, tool: 2 } as const
  const rows: GrantRow[] = grants.value.map((grant) => {
    const kind = kindOf(grant.gw)
    const server = kind === 'tool' ? toolByName.value.get(grant.gw) : undefined
    return {
      ...grant,
      kind,
      serverName: server ? (servers.value.find((s) => s.id === server.server_id)?.name ?? '') : undefined,
    }
  })
  return rows.sort((a, b) => rank[a.kind] - rank[b.kind] || a.gw.localeCompare(b.gw))
})

const graphServers = computed<ServerRow[]>(() => {
  const granted = new Set<string>()
  for (const grant of grants.value) if (!grant.gw.includes('*')) granted.add(grant.gw)
  const rows: ServerRow[] = []
  for (const server of servers.value) {
    const list = (toolsByServer.value.get(server.id) ?? []).filter((tool) => granted.has(tool.gateway_name))
    if (list.length > 0) rows.push({ server, tools: list })
  }
  return rows
})

// 画布占位：没有精确工具授权时保留一行（空态）
const serverRows = computed<ServerRow[]>(() =>
  graphServers.value.length > 0 ? graphServers.value : [{ server: null, tools: [] }],
)

function rowHot(row: ServerRow) {
  return row.tools.some((tool) => tool.gateway_name === focusTool.value || tool.gateway_name === hover.value)
}

function tagLabel(kind: GrantKind) {
  if (kind === 'global') return 'GLOBAL RULE'
  if (kind === 'pattern') return 'SERVER RULE'
  return 'TOOL'
}

function serverDotClass(server: MCPServer): string {
  if (!server.enabled) return 'idle'
  if (server.health_status === 'healthy') return 'ok'
  if (server.health_status === 'unhealthy') return 'bad'
  return 'idle'
}

function healthLabel(server: MCPServer | null): string {
  if (!server) return ''
  if (!server.enabled) return t('access.healthDisabled')
  if (server.health_status === 'healthy') return t('access.healthOk')
  if (server.health_status === 'unhealthy') return t('access.healthBad')
  return t('access.healthUnknown')
}

// ---- 授权操作 ----
function isGranted(name: string) {
  return grants.value.some((grant) => grant.gw === name)
}

function toggleTool(tool: Tool) {
  const i = grants.value.findIndex((grant) => grant.gw === tool.gateway_name)
  if (i >= 0) {
    const gw = grants.value[i].gw
    grants.value.splice(i, 1)
    if (focusTool.value === gw) {
      focusTool.value = ''
      delete openGws[gw]
    }
  } else {
    const gw = tool.gateway_name
    grants.value.push({ gw, headers: [], defargs: [] })
    focusAndOpen(gw)
  }
}

function removeGrant(row: GrantRow) {
  const i = grants.value.findIndex((grant) => grant.gw === row.gw)
  if (i < 0) return
  const gw = grants.value[i].gw
  grants.value.splice(i, 1)
  if (focusTool.value === gw) {
    focusTool.value = ''
    delete openGws[gw]
  }
}

function focusAndOpen(gw: string) {
  focusTool.value = gw
  openGws[gw] = true
}

function toggleCard(gw: string) {
  focusTool.value = gw
  if (openGws[gw]) delete openGws[gw]
  else openGws[gw] = true
}

// ---- 数据加载 / 保存 ----
function parseArg(raw: string): unknown {
  try {
    return raw.trim() ? JSON.parse(raw) : ''
  } catch {
    return raw
  }
}

function serialize() {
  return JSON.stringify({
    qps: form.qps,
    burst: form.burst,
    enabled: form.enabled,
    grants: grants.value.map((g) => [
      g.gw,
      g.headers.map((h) => `${h.k}${h.v}`),
      g.defargs.map((a) => `${a.k}${a.v}`),
    ]),
  })
}

const dirty = computed(() => Boolean(baseline) && baseline !== serialize())

async function load() {
  loading.value = true
  try {
    const [loadedKey, allTools, allServers] = await Promise.all([
      getKey(String(route.params.id)),
      listAllTools(),
      listAllServers(),
    ])
    key.value = loadedKey
    tools.value = allTools
    servers.value = allServers
    Object.assign(form, { qps: loadedKey.qps, burst: loadedKey.burst, enabled: loadedKey.enabled })
    grants.value = loadedKey.grants.map((grant) => ({
      gw: grant.gateway_name,
      headers: Object.entries(grant.headers ?? {}).map(([k, v]) => ({ k, v })),
      defargs: Object.entries(grant.default_args ?? {}).map(([k, v]) => ({ k, v: JSON.stringify(v) })),
    }))
    baseline = serialize()
  } catch (e) {
    message.error(String(e))
    router.push('/access')
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!key.value) return
  saving.value = true
  try {
    const payload: ToolGrant[] = grants.value.map((grant) => ({
      gateway_name: grant.gw,
      headers: Object.fromEntries(grant.headers.filter((item) => item.k).map((item) => [item.k, item.v])),
      default_args: Object.fromEntries(grant.defargs.filter((item) => item.k).map((item) => [item.k, parseArg(item.v)])),
    }))
    key.value = await updateKey(key.value.id, {
      qps: form.qps,
      burst: form.burst,
      enabled: form.enabled,
      grants: payload,
    })
    baseline = serialize()
    message.success(t('access.savedOk'))
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}

async function doRotateKey() {
  if (!key.value) return
  rotating.value = true
  try {
    const res = await rotateKey(key.value.id)
    key.value = res
    newSecret.value = res.secret ?? ''
    rotateVisible.value = false
    secretVisible.value = true
  } catch (e) {
    message.error(String(e))
  } finally {
    rotating.value = false
  }
}

function openInvoke() {
  invokeTool.value = undefined
  invokeArgsText.value = ''
  invokeOut.value = null
  invokeVisible.value = true
}

async function runInvoke() {
  const ak = key.value
  const tool = invokeTool.value
  if (!ak || !tool) return
  let args: Record<string, unknown> = {}
  if (invokeArgsText.value.trim()) {
    try {
      const v = JSON.parse(invokeArgsText.value)
      if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error('obj')
      args = v
    } catch {
      message.warning(t('access.invokeArgsInvalid'))
      return
    }
  }
  invokeRunning.value = true
  invokeOut.value = null
  try {
    const res = await invokeKey(ak.id, { gateway_tool: tool, arguments: args })
    invokeOut.value = { type: 'ok', res }
  } catch (e) {
    // 403 授权拒绝/禁用等由后端信封给出；展示为明确拒绝而非裸错误。
    invokeOut.value = { type: 'denied', message: String(e) }
  } finally {
    invokeRunning.value = false
  }
}

async function copyNewSecret() {
  try {
    await navigator.clipboard.writeText(newSecret.value)
    message.success(t('access.copied'))
  } catch {
    message.warning(t('access.copyFailed'))
  }
}

onMounted(load)
</script>

<style scoped>
/* ===== 页面骨架 ===== */
.akd-root { width: 100%; }
.akd {
  max-width: 1500px;
  margin: 0 auto;
  color: var(--mc-ink);
}
.akd :deep(.ant-input), .akd :deep(.ant-input-number) { font-size: 12px; }

/* ===== Header ===== */
.ph {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 14px;
}
.ph-main { min-width: 0; flex: 1; }
.ph-meta { display: flex; align-items: center; gap: 8px; margin-bottom: 2px; }
.ph-back { margin-left: -8px; padding: 0 6px; height: 24px; font-size: 12px; }
.ph-eyebrow {
  font-family: var(--mc-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.12em;
  color: var(--mc-accent);
}
.ph-eyebrow::after { content: '/'; margin: 0 6px 0 8px; color: #9aa7ba; font-weight: 400; letter-spacing: 0; }
.ph-eyebrow-var { font-size: 11px; color: var(--mc-ink-2); }
.ph-titleline { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; }
.ph-name { margin: 0; font-size: 22px; font-weight: 650; line-height: 1.25; color: var(--mc-ink); letter-spacing: 0.01em; }
.ph-sub { margin: 0; font-size: 12.5px; color: var(--mc-ink-2); }

.ph-side { display: flex; flex-direction: column; align-items: flex-end; gap: 8px; }
.ph-states { display: flex; gap: 8px; }
.ph-btns { display: flex; gap: 8px; }

.state-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 22px;
  padding: 0 8px;
  border: 1px solid var(--mc-line);
  border-radius: 20px;
  background: #fff;
  font-size: 11px;
  color: var(--mc-ink-2);
}
.state-chip.dirty { border-color: rgba(217, 130, 43, 0.45); background: rgba(217, 130, 43, 0.06); color: var(--mc-warn); }
.state-dot { width: 7px; height: 7px; border-radius: 50%; display: inline-block; }
.state-dot.ok { background: var(--mc-ok); box-shadow: 0 0 0 3px rgba(18, 161, 80, 0.14); }
.state-dot.off { background: var(--mc-ink-3); box-shadow: 0 0 0 3px rgba(138, 148, 163, 0.14); }
.state-dot.warn { background: var(--mc-warn); box-shadow: 0 0 0 3px rgba(217, 130, 43, 0.14); }

/* ===== Key 基础策略卡 ===== */
.base {
  display: flex;
  align-items: stretch;
  margin-bottom: 14px;
  min-height: 78px;
  border: 1px solid var(--mc-line);
  border-radius: 10px;
  background: #fff;
  box-shadow: 0 1px 2px rgba(13, 21, 32, 0.04);
  overflow: hidden;
}
.base-title {
  display: flex;
  align-items: center;
  flex: 0 0 auto;
  margin: 0;
  padding: 0 18px;
  font-size: 13px;
  font-weight: 600;
  border-right: 1px solid var(--mc-line-soft);
  color: var(--mc-ink);
  white-space: nowrap;
}
.bf {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  flex: 1 1 auto;
  padding: 10px 18px;
  border-right: 1px solid var(--mc-line-soft);
}
.bf:last-child { border-right: none; }
.bf-label {
  font-family: var(--mc-mono);
  font-size: 9.5px;
  letter-spacing: 0.09em;
  color: var(--mc-ink-3);
}
.bf-value { font-size: 13px; color: var(--mc-ink); line-height: 1; }
.bf-subject { font-weight: 650; }
.bf-num { width: 108px; }
.bf-toggle { display: flex; align-items: center; gap: 8px; min-height: 22px; }
.bf-toggle-txt { font-size: 12px; color: var(--mc-ok); }
.bf-toggle-txt.off { color: var(--mc-ink-3); }

/* ===== 三栏工作区 ===== */
.work {
  display: grid;
  grid-template-columns: minmax(238px, 24%) minmax(0, 1fr) minmax(258px, 26%);
  grid-template-rows: minmax(0, 1fr);
  height: calc(100vh - 318px);
  min-height: 470px;
  border: 1px solid var(--mc-line);
  border-radius: 12px;
  background: #fff;
  box-shadow: 0 1px 2px rgba(13, 21, 32, 0.04), 0 16px 40px -22px rgba(13, 21, 32, 0.22);
  overflow: hidden;
}

.pane { display: flex; flex-direction: column; min-height: 0; background: #fff; }
.pane + .pane { border-left: 1px solid var(--mc-line-soft); }
.pane--graph { background: #f8fafc; }

.pane-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex: 0 0 auto;
  padding: 11px 14px 10px;
  border-bottom: 1px solid var(--mc-line-soft);
  background: #fff;
}
.pane-head-txt { min-width: 0; }
.pane-kicker {
  margin: 0 0 2px;
  font-family: var(--mc-mono);
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.11em;
  color: var(--mc-accent);
  white-space: nowrap;
}
.pane-title { margin: 0; font-size: 14px; font-weight: 600; color: var(--mc-ink); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.pane-count {
  flex: 0 0 auto;
  padding: 1px 8px;
  border-radius: 10px;
  background: #eef1f6;
  font-size: 11px;
  color: var(--mc-ink-2);
}

/* 状态点 */
.sdot { width: 8px; height: 8px; border-radius: 50%; flex: 0 0 auto; display: inline-block; }
.sdot.ok { background: var(--mc-ok); }
.sdot.bad { background: var(--mc-danger); }
.sdot.idle { background: #b8c2d0; }

/* ===== 左：目录 ===== */
.cat-tools { display: flex; flex-direction: column; min-height: 0; flex: 1; padding: 0 10px 10px; }
.cat-search { margin: 10px 0 6px; }
.cat-scroll { flex: 1; min-height: 0; overflow: auto; padding: 2px 2px 4px; }
.cat-note { margin: 12px 6px; color: var(--mc-ink-3); font-size: 12px; line-height: 1.6; }
.cat-foot { margin: 8px 6px 2px; color: var(--mc-ink-3); font-size: 11px; line-height: 1.55; }

.sg { margin: 9px 0; }
.sg-head {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 4px 6px;
}
.sg-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12.5px; font-weight: 600; color: var(--mc-ink); }
.sg-count { font-size: 10.5px; color: var(--mc-ink-3); background: #f0f3f7; border-radius: 8px; padding: 0 6px; line-height: 16px; }

.ctool {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  margin: 2px 0;
  padding: 7px 8px;
  border: 1px solid transparent;
  border-radius: 7px;
  background: transparent;
  cursor: pointer;
  text-align: left;
  transition: background 0.14s ease, border-color 0.14s ease, box-shadow 0.14s ease;
}
.ctool:hover { background: #f3f6fa; }
.ctool.granted { background: var(--mc-accent-soft); }
.ctool.cur {
  background: var(--mc-accent-soft);
  border-color: var(--mc-accent);
  box-shadow: inset 3px 0 0 var(--mc-accent);
}
.ctool-ck {
  flex: 0 0 17px;
  height: 17px;
  line-height: 15px;
  border: 1px solid #c3cfdd;
  border-radius: 50%;
  text-align: center;
  font-size: 11px;
  color: transparent;
  transition: all 0.14s ease;
}
.ctool.granted .ctool-ck { background: var(--mc-accent); border-color: var(--mc-accent); color: #fff; }
.ctool:not(.granted):hover .ctool-ck { color: var(--mc-accent); border-color: var(--mc-accent); }
.ctool-tx { flex: 1; min-width: 0; }
.ctool-tx b { display: block; font-size: 11.5px; font-weight: 500; color: var(--mc-ink); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ctool-tx i { display: block; margin-top: 2px; font-style: normal; font-size: 11px; color: var(--mc-ink-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ctool.granted .ctool-tx b { font-weight: 650; }

/* ===== 中：拓扑画布 ===== */
.canvas {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: grid;
  place-items: center;
  padding: 18px 14px;
  background-image: radial-gradient(circle, rgba(96, 122, 158, 0.16) 1px, transparent 1px);
  background-size: 16px 16px;
}

.topo {
  display: grid;
  grid-template-columns: auto 22px auto 22px minmax(228px, max-content);
  justify-content: center;
  align-items: stretch;
  margin: auto;
  min-height: 0;
}
.g-node { position: relative; width: 100%; min-height: 0; }
.g-slot { grid-column: 5; min-width: 0; display: flex; align-items: center; }
.g-slot > * { width: 100%; }

/* —— 统一节点卡 —— */
.g-node {
  border: 1px solid var(--mc-line);
  border-radius: 10px;
  background: #fff;
  box-shadow: 0 1px 2px rgba(13, 21, 32, 0.04);
  padding: 13px 14px 12px;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}
.g-node--key { grid-column: 1; grid-row: 1 / -1; place-self: center; width: 158px; }
.g-node--allowlist { grid-column: 3; grid-row: 1 / -1; place-self: center; width: 178px; }
.g-node--server { border-color: var(--mc-line); }
.g-node.hot {
  border-color: rgba(31, 111, 235, 0.55);
  box-shadow: 0 0 0 3px rgba(31, 111, 235, 0.08), 0 8px 18px -12px rgba(31, 111, 235, 0.35);
}
.g-node--key.g-node.hot { border-color: var(--mc-accent); }

.g-eyebrow {
  margin: 0 0 7px;
  font-family: var(--mc-mono);
  font-size: 9.5px;
  font-weight: 650;
  letter-spacing: 0.1em;
  color: var(--mc-accent);
}
.g-name { margin: 0; font-size: 15px; font-weight: 650; color: var(--mc-ink); line-height: 1.3; }
.g-sub { margin: 3px 0 0; font-size: 11px; color: var(--mc-ink-2); }
.g-desc { margin: 4px 0 0; font-size: 11px; line-height: 1.5; color: var(--mc-ink-3); }

.g-chips { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 10px; }
.g-chip {
  display: inline-flex;
  align-items: baseline;
  gap: 4px;
  padding: 2px 7px;
  border: 1px solid var(--mc-line);
  border-radius: 12px;
  background: #f6f8fb;
  font-size: 10.5px;
  color: var(--mc-ink-2);
  white-space: nowrap;
}
.g-chip b { font-size: 10.5px; color: var(--mc-ink); }
.g-chip.acc { border-color: transparent; background: var(--mc-accent-soft); color: var(--mc-accent-strong); }
.g-chip.acc b { color: var(--mc-accent-strong); }
.g-chip.on { color: var(--mc-ok); }
.g-chip.mode { background: #fff; }

/* —— Server 节点 —— */
.gs-head { display: flex; align-items: center; gap: 8px; padding-bottom: 9px; border-bottom: 1px dashed var(--mc-line); }
.gs-txt { flex: 1; min-width: 0; }
.gs-name { display: block; font-size: 13.5px; font-weight: 650; color: var(--mc-ink); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.gs-health { display: block; font-size: 10.5px; color: var(--mc-ink-3); }
.gs-health.ok { color: var(--mc-ok); }
.gs-health.bad { color: var(--mc-danger); }
.gs-count { font-size: 10px; color: var(--mc-ink-3); background: #f0f3f7; border-radius: 8px; padding: 0 6px; line-height: 15px; }

.g-tools { list-style: none; margin: 6px 0 0; padding: 0; }
.g-tool {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 3px 0;
  padding: 5px 7px;
  border: 1px solid transparent;
  border-radius: 6px;
  font-size: 11.5px;
  color: var(--mc-ink);
  cursor: pointer;
  transition: background 0.14s ease, border-color 0.14s ease;
  white-space: nowrap;
  overflow: hidden;
}
.g-tool .mono { overflow: hidden; text-overflow: ellipsis; }
.g-tool:hover { background: #f3f6fa; }
.g-tool.cur {
  border-color: var(--mc-accent);
  background: var(--mc-accent-soft);
}
.g-tool-ck { color: var(--mc-accent); font-size: 11px; font-weight: 700; }

.g-empty { border: 1px dashed var(--mc-line); border-radius: 10px; background: rgba(255, 255, 255, 0.6); padding: 22px 18px; text-align: center; }
.g-empty-title { margin: 0; color: var(--mc-ink-2); font-size: 12.5px; }
.g-empty-hint { margin: 6px 0 0; color: var(--mc-ink-3); font-size: 11px; line-height: 1.6; }

/* —— 箭头 —— */
.g-arrow-cell { display: flex; align-items: center; }
.g-arrow-cell--span { grid-column: 2; grid-row: 1 / -1; justify-content: center; }
.g-arrow-cell--row { grid-column: 4; }
.g-arrow {
  position: relative;
  display: block;
  width: 100%;
  height: 2px;
  border-radius: 2px;
  background: #c8d2de;
  transition: background 0.15s ease;
}
.g-arrow::after {
  content: '';
  position: absolute;
  right: -2px;
  top: 50%;
  width: 0;
  height: 0;
  transform: translateY(-50%);
  border-top: 3px solid transparent;
  border-bottom: 3px solid transparent;
  border-left: 5px solid #c8d2de;
  transition: border-color 0.15s ease;
}
.g-arrow-cell.hot .g-arrow { background: var(--mc-accent); }
.g-arrow-cell.hot .g-arrow::after { border-left-color: var(--mc-accent); }

.graph-foot {
  flex: 0 0 auto;
  padding: 7px 14px;
  border-top: 1px solid var(--mc-line-soft);
  background: #fff;
  font-size: 11px;
  color: var(--mc-ink-3);
}

/* ===== 右：授权配置 ===== */
.cfg-scroll { flex: 1; min-height: 0; overflow: auto; padding: 10px; }
.cfg-empty { margin: 14px 4px; }

.gc {
  margin-bottom: 8px;
  border: 1px solid var(--mc-line);
  border-radius: 9px;
  background: #fff;
  overflow: hidden;
  transition: border-color 0.14s ease, box-shadow 0.14s ease;
}
.gc:hover { border-color: #bccadb; }
.gc.cur {
  border-color: var(--mc-accent);
  box-shadow: 0 0 0 2px rgba(31, 111, 235, 0.1);
  background: #fbfdff;
}
.gc-head {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 8px 9px;
  cursor: pointer;
  user-select: none;
}
.gc-tag {
  flex: 0 0 auto;
  font-family: var(--mc-mono);
  font-size: 8.5px;
  font-weight: 650;
  letter-spacing: 0.07em;
  padding: 2px 5px;
  border-radius: 4px;
  color: var(--mc-accent-strong);
  background: var(--mc-accent-soft);
}
.gc-tag.global { color: var(--mc-warn); background: rgba(217, 130, 43, 0.1); }
.gc-tag.pattern { color: #5a6980; background: #eef1f6; }
.gc-name {
  flex: 1;
  min-width: 0;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--mc-ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.gc-spacer { flex: 0 0 auto; }

.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--mc-ink-3);
  cursor: pointer;
  font-size: 13px;
  line-height: 1;
  transition: color 0.12s ease, background 0.12s ease;
}
.icon-btn.del { font-size: 13px; }
.icon-btn.del:hover { color: var(--mc-danger); background: rgba(224, 69, 60, 0.1); }
.icon-btn.x { flex: 0 0 18px; }
.icon-btn.x:hover { color: var(--mc-danger); }
.gc-chev {
  flex: 0 0 auto;
  font-size: 10px;
  color: var(--mc-ink-3);
  transition: transform 0.18s ease;
}
.gc-chev.open { transform: rotate(180deg); color: var(--mc-accent); }

.gc-src {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  padding: 0 12px 7px 30px;
  font-size: 10.5px;
  color: var(--mc-ink-3);
}
.gc-src--open { padding: 2px 12px 6px 30px; }

.gc-body { padding: 0 10px 8px; border-top: 1px dashed var(--mc-line-soft); }
.gc-blk { padding-top: 8px; }
.gc-blk-title { display: flex; align-items: baseline; gap: 6px; margin: 0 0 6px; font-size: 11px; color: var(--mc-ink-2); }
.gc-blk-title b { color: var(--mc-ink-3); font-size: 10.5px; }
.pair { display: grid; grid-template-columns: 1fr 1fr 18px; gap: 5px; margin: 5px 0; }
.blk-add { padding-left: 0; height: 22px; font-size: 11.5px; }

/* 展开过渡 */
.grow-enter-active,
.grow-leave-active { transition: opacity 0.18s ease, transform 0.18s ease; transform-origin: top center; }
.grow-enter-from,
.grow-leave-to { opacity: 0; transform: translateY(-4px); }

/* ===== 密钥提示与重置 ===== */
.secret-note {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
  padding: 8px 12px;
  border: 1px solid var(--mc-line-soft);
  border-radius: 8px;
  background: #f7f9fc;
}
.sn-ic { flex: 0 0 auto; display: flex; align-items: center; font-size: 14px; color: var(--mc-accent); }
.sn-tx { flex: 1; min-width: 0; }
.sn-title { margin: 0 0 1px; font-size: 12px; font-weight: 600; color: var(--mc-ink-2); }
.sn-body { margin: 0; font-size: 11.5px; line-height: 1.5; color: var(--mc-ink-2); }
.sn-code { display: flex; align-items: baseline; flex-wrap: wrap; gap: 6px; }
.sn-code code { font: 11px var(--mc-mono); color: var(--mc-ink); background: #eef1f6; border: 1px solid var(--mc-line-soft); border-radius: 4px; padding: 1px 5px; }
.sn-alt { color: var(--mc-ink-3); }
.sn-rotate { flex: 0 0 auto; }
.rotate-desc { margin: 0 0 2px; color: var(--mc-ink-2); font-size: 13px; line-height: 1.6; }
.mb { margin-bottom: 16px; }
.secret-row { display: flex; width: 100%; }

/* ===== 试调用 modal ===== */
.inv-desc { margin: 0 0 6px; color: var(--mc-ink-2); font-size: 12px; line-height: 1.6; }
.inv-actions { margin: 2px 0 10px; }
.inv-result { margin-top: 4px; }
.inv-meta { font-size: 11.5px; color: var(--mc-ink-2); margin: 10px 0 6px; }
.inv-content {
  margin: 0;
  padding: 10px 12px;
  background: #f6f8fb;
  border: 1px solid var(--mc-line);
  border-radius: 8px;
  font-size: 12px;
  color: var(--mc-ink);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 260px;
  overflow: auto;
}
.muted { color: var(--mc-ink-3); font-size: 12px; }

/* ===== 响应式 ===== */
@media (max-width: 1240px) {
  .work {
    display: flex;
    flex-direction: column;
    height: auto;
    min-height: 0;
    overflow: visible;
  }
  .pane { flex: none; min-height: 0; }
  .pane--catalog { order: 1; height: 340px; }
  .pane--graph { order: 2; height: 560px; }
  .pane--cfg { order: 3; border-left: none; border-top: 1px solid var(--mc-line-soft); height: 420px; }
  .work .pane + .pane { border-left: none; }
}
@media (max-width: 760px) {
  .ph { flex-direction: column; align-items: flex-start; gap: 10px; }
  .ph-side { flex-direction: row; align-items: center; width: 100%; justify-content: space-between; flex-wrap: wrap; }
  .base { flex-wrap: wrap; }
  .base-title { width: 100%; border-right: none; border-bottom: 1px solid var(--mc-line-soft); min-height: 40px; }
  .bf { flex: 1 1 45%; border-right: none; }
  .bf:nth-child(odd) { border-right: none; }
}
</style>
