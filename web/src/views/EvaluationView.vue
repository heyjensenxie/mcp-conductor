<template>
  <div>
    <!-- 工具栏（不打印） -->
    <a-card :bordered="true" class="mb no-print">
      <a-space wrap>
        <a-select v-model:value="serverId" :placeholder="t('evaluation.selectServer')" style="width: 240px" show-search option-filter-prop="label" @change="onServerChange">
          <a-select-option v-for="s in servers" :key="s.id" :value="s.id" :label="s.name">{{ s.name }} ({{ primaryEndpoint(s) }})</a-select-option>
        </a-select>
        <a-select v-if="currentInstances.length > 1" v-model:value="targetInstance" style="width: 200px" allow-clear :placeholder="t('evaluation.instanceAuto')" @change="onTargetChange">
          <a-select-option v-for="inst in currentInstances" :key="inst.id" :value="inst.id">
            {{ inst.id }}<template v-if="!inst.enabled"> · {{ t('servers.disabled') }}</template>
          </a-select-option>
        </a-select>
        <a-button type="primary" :disabled="!serverId" :loading="qualityLoading" @click="runQuality">
          <template #icon><AuditOutlined /></template>{{ t('evaluation.runQuality') }}
        </a-button>
        <a-button :disabled="!report" @click="exportPdf">
          <template #icon><FilePdfOutlined /></template>{{ t('evaluation.exportPdf') }}
        </a-button>
      </a-space>
    </a-card>

    <!-- Meta 概况（不打印） -->
    <a-card v-if="meta" :bordered="true" size="small" class="mb no-print">
      <a-space wrap :size="18">
        <span class="meta-item"><b>{{ t('evaluation.metaTools') }}</b>: {{ meta.stored_tool_count }}</span>
        <span class="meta-item"><b>{{ t('evaluation.metaRuntime') }}</b>:
          <a-tag :color="meta.runtime_metrics_available ? 'green' : 'default'" :bordered="false">{{ meta.runtime_metrics_available ? t('common.yes') : t('common.no') }}</a-tag>
        </span>
        <span class="meta-item"><b>{{ t('evaluation.metaOverrides') }}</b>: {{ meta.platform_override_count }}</span>
        <span class="meta-item">
          <b>{{ t('evaluation.metaProbe') }}</b>:
          <template v-if="meta.probe"><span class="mono">{{ meta.probe.id }}</span> · <span class="mono">{{ meta.probe.endpoint }}</span></template>
          <template v-else><a-tag color="orange">{{ t('evaluation.noProbe') }}</a-tag></template>
        </span>
      </a-space>
      <div v-if="meta.issues && meta.issues.length" class="issues">
        <div v-for="(iss, i) in meta.issues" :key="i" class="issue">
          <a-tag :color="severityColor(iss.severity)" :bordered="false">{{ iss.code }}</a-tag>
          <span>{{ iss.message }}</span>
          <span v-if="iss.suggestion" class="muted">→ {{ iss.suggestion }}</span>
        </div>
      </div>
    </a-card>

    <!-- 质量报告（打印内容） -->
    <a-card v-if="report" :bordered="true" class="mb report-area">
      <div class="report-head">
        <span class="score-big mono">{{ scoreText(report.overall_score) }}</span>
        <div class="report-head-side">
          <div class="report-title">{{ t('evaluation.overallScore') }}</div>
          <div class="report-sub">{{ report.server.name }} · {{ t('evaluation.generatedAt') }} {{ formatTime(report.generated_at) }}</div>
          <div v-if="report.server.probed_instance" class="runtime-note">
            {{ t('evaluation.probedTarget') }}: <span class="mono">{{ report.server.probed_instance.id }}</span>
          </div>
          <div v-if="report.runtime && !report.runtime.available" class="runtime-note">{{ t('evaluation.runtimeTrafficNone') }}</div>
          <div v-if="report.platform_overrides" class="runtime-note">{{ t('evaluation.overrideHint', { count: report.platform_overrides.count }) }}</div>
        </div>
      </div>

      <!-- 运行时流量块：有真实调用时给出观测口径的成功率/延迟（评测结论的一部分） -->
      <div v-if="report.runtime && report.runtime.available" class="runtime-stats">
        <span class="stat"><b>{{ t('evaluation.runtimeTotals') }}</b> {{ report.runtime.totals }}</span>
        <span class="stat"><b>{{ t('evaluation.runtimeSuccessRate') }}</b> {{ rateText(report.runtime.success_rate) }}</span>
        <span class="stat"><b>{{ t('evaluation.runtimeP95') }}</b> {{ msText(report.runtime.p95) }}</span>
        <span class="stat bad"><b>{{ t('evaluation.runtimeErrors') }}</b> {{ report.runtime.errors }}</span>
      </div>

      <a-divider orientation="left">{{ t('evaluation.dimensions') }}</a-divider>
      <a-row :gutter="[12, 12]">
        <a-col v-for="d in report.dimensions" :key="d.key" :span="4">
          <div class="dim-tile">
            <div class="dim-name">{{ dimLabel(d.key) }}</div>
            <div v-if="d.available" class="dim-score mono">{{ scoreText(d.score) }}</div>
            <div v-else class="dim-score dim-na">n/a</div>
            <a-progress
              v-if="d.available"
              :percent="d.score"
              :show-info="false"
              :stroke-color="scoreColor(d.score)"
              size="small"
            />
            <div v-else class="dim-note">{{ t('evaluation.dimNA') }}</div>
            <div class="dim-weight">
              {{ t('evaluation.weight') }} {{ pctText(d.weight) }} ·
              {{ t('evaluation.contribution') }} <span class="mono">{{ (d.weighted_contribution ?? 0).toFixed(1) }}</span>
            </div>
          </div>
        </a-col>
      </a-row>

      <a-divider orientation="left">{{ t('evaluation.toolChecks') }}</a-divider>
      <a-table
        :data-source="report.tool_checks"
        :columns="toolColumns"
        :pagination="false"
        size="small"
        :row-key="(r: any) => r.tool"
        :expand-row-by-click="true"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'schema'">
            <a-tag :color="scoreTag(record.scores.schema)" :bordered="false">{{ scoreText(record.scores.schema) }}</a-tag>
          </template>
          <template v-else-if="column.key === 'description'">
            <a-tag :color="scoreTag(record.scores.description)" :bordered="false">{{ scoreText(record.scores.description) }}</a-tag>
          </template>
          <template v-else-if="column.key === 'naming'">
            <a-tag :color="scoreTag(record.scores.naming)" :bordered="false">{{ scoreText(record.scores.naming) }}</a-tag>
          </template>
          <template v-else-if="column.key === 'findings'">
            <a-tag v-if="record.findings && record.findings.length" color="orange">{{ record.findings.length }}</a-tag>
            <a-tag v-else color="green">{{ t('evaluation.noFindings') }}</a-tag>
          </template>
        </template>
        <template #expandedRowRender="{ record }">
          <ul v-if="record.findings && record.findings.length" class="finding-list">
            <li v-for="(f, i) in record.findings" :key="i">
              <a-tag :color="severityColor(f.severity)" :bordered="false" class="finding-tag">{{ f.code }}</a-tag>
              <span>{{ f.message }}</span>
              <div v-if="f.suggestion" class="finding-suggestion">→ {{ f.suggestion }}</div>
            </li>
          </ul>
          <div v-else class="muted">{{ t('evaluation.noFindings') }}</div>
        </template>
      </a-table>
    </a-card>
    <a-empty v-else-if="serverId && !qualityLoading" :description="t('evaluation.noReport')" />

    <!-- 回归用例（编辑区不打印） -->
    <a-card :bordered="true" class="mb">
      <template #title>{{ t('evaluation.regression') }}</template>
      <template #extra>
        <a-space>
          <a-button size="small" type="primary" :disabled="!serverId || !suiteCases.length" :loading="suiteLoading" @click="runSuite">
            <template #icon><PlayCircleOutlined /></template>{{ t('evaluation.runSuite') }}
          </a-button>
          <a-button size="small" :disabled="!serverId" @click="addCase">
            <template #icon><PlusOutlined /></template>{{ t('evaluation.addCase') }}
          </a-button>
        </a-space>
      </template>

      <div v-if="suiteCases.length" class="case-rows">
        <div v-for="(c, i) in suiteCases" :key="i" class="case-row no-print">
          <a-input v-model:value="c.name" :placeholder="t('evaluation.caseName')" class="case-name" />
          <a-input v-model:value="c.gateway_tool" placeholder="mock.search" class="case-tool" />
          <a-textarea v-model:value="c.arguments_text" :rows="1" :placeholder='{ "q": "x" }' class="case-args" />
          <a-input v-model:value="c.expected_substring" :placeholder="t('evaluation.expectedSubstring')" class="case-expect" />
          <a-input-number v-model:value="c.timeout_ms" :min="0" :step="500" class="case-timeout" :placeholder="t('evaluation.timeoutMs')" @change="persistCases" />
          <a-button type="text" danger size="small" @click="suiteCases.splice(i, 1)">
            <template #icon><DeleteOutlined /></template>
          </a-button>
        </div>
      </div>
      <a-empty v-else :description="t('evaluation.noCasesHint')" />
      <p class="muted no-print">{{ t('evaluation.qualityHint') }}</p>
    </a-card>

    <!-- 回归结果（打印内容） -->
    <a-card v-if="suiteResult" :bordered="true" class="mb report-area">
      <a-divider orientation="left">{{ t('evaluation.suiteSummary') }}</a-divider>
      <a-space wrap :size="24" class="mb">
        <span class="stat"><b>{{ t('evaluation.total') }}</b> {{ suiteResult.summary.total }}</span>
        <span class="stat ok"><b>{{ t('evaluation.passed') }}</b> {{ suiteResult.summary.passed }}</span>
        <span class="stat bad"><b>{{ t('evaluation.failed') }}</b> {{ suiteResult.summary.failed }}</span>
        <span class="stat"><b>{{ t('evaluation.passRate') }}</b> {{ rateText(suiteResult.summary.pass_rate) }}</span>
        <span class="stat"><b>{{ t('evaluation.avgLatency') }}</b> {{ msText(suiteResult.summary.avg_latency_ms) }}</span>
        <span class="stat"><b>{{ t('evaluation.p95Latency') }}</b> {{ msText(suiteResult.summary.p95_latency_ms) }}</span>
      </a-space>

      <a-divider orientation="left">{{ t('evaluation.perCase') }}</a-divider>
      <a-table :data-source="suiteResult.cases" :columns="caseColumns" :pagination="false" size="small" :row-key="(r: any) => r.name">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'result'">
            <a-tag :color="record.passed ? 'green' : 'red'" :bordered="false">
              {{ record.passed ? t('evaluation.passLabel') : t('evaluation.failLabel') }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'latency'">
            <span class="mono">{{ msText(record.latency_ms) }}</span>
          </template>
          <template v-else-if="column.key === 'output'">
            <span class="mono snippet">{{ record.output_snippet || record.error || '-' }}</span>
          </template>
        </template>
      </a-table>

      <!-- 按工具分组：回归用例失败常聚在某工具（Schema/入参不稳），一眼定位 -->
      <a-divider v-if="suiteResult.by_tool && suiteResult.by_tool.length" orientation="left">{{ t('evaluation.byTool') }}</a-divider>
      <a-table v-if="suiteResult.by_tool && suiteResult.by_tool.length" :data-source="suiteResult.by_tool" :columns="byToolColumns" :pagination="false" size="small" :row-key="(r: any) => r.gateway_tool">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'passed'">
            <a-tag :color="record.passed === record.total ? 'green' : 'red'" :bordered="false">{{ record.passed }}/{{ record.total }}</a-tag>
          </template>
          <template v-else-if="column.key === 'p95'">
            <span class="mono">{{ msText(record.p95_latency_ms) }}</span>
          </template>
        </template>
      </a-table>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { message } from 'ant-design-vue'
import { AuditOutlined, DeleteOutlined, FilePdfOutlined, PlayCircleOutlined, PlusOutlined } from '@ant-design/icons-vue'
import { getEvalMeta, listAllServers, primaryEndpoint, runEvalQuality, runEvalSuite } from '@/api'
import type { EvalMeta, EvalReport, EvalSuiteResult, MCPServer, ServerInstance } from '@/types'

const { t } = useI18n()

const servers = ref<MCPServer[]>([])
// 下拉未选保持 undefined：antd Select 仅在值为空(null/undefined)时展示占位文案。
const serverId = ref<string | undefined>(undefined)
// 评测定向实例：空=取首个可拨测实例（仅在该 Server 多实例时显示选择器）。
const targetInstance = ref<string | undefined>(undefined)
const meta = ref<EvalMeta>()
const report = ref<EvalReport>()
const qualityLoading = ref(false)
const suiteLoading = ref(false)

// 当前选中 Server 的实例（用于实例定向下拉）。
const currentInstances = computed<ServerInstance[]>(() => servers.value.find((s) => s.id === serverId.value)?.instances ?? [])

// ---- 回归用例编辑（浏览器 localStorage 暂存）----
interface CaseEdit {
  name: string
  gateway_tool: string
  arguments_text: string
  expected_substring: string
  timeout_ms?: number
}
const suiteCases = reactive<CaseEdit[]>([])
const suiteResult = ref<EvalSuiteResult>()

const storageKey = () => (serverId.value ? `eval.suite.${serverId.value}` : '')

onMounted(async () => {
  try {
    servers.value = await listAllServers()
  } catch (e) {
    message.error(String(e))
  }
})

async function onServerChange() {
  targetInstance.value = undefined
  meta.value = undefined
  report.value = undefined
  suiteResult.value = undefined
  loadStoredCases()
  await loadMeta()
}

async function loadMeta() {
  if (!serverId.value) return
  try {
    meta.value = await getEvalMeta(serverId.value, targetInstance.value || undefined)
  } catch (e) {
    message.error(String(e))
  }
}

// 切换评测定向实例：重置结果并刷新概况（选定实例不可拨测时后端给出明确错误）。
async function onTargetChange() {
  report.value = undefined
  suiteResult.value = undefined
  await loadMeta()
}

watch(suiteCases, () => persistCases(), { deep: true })

function loadStoredCases() {
  suiteCases.splice(0, suiteCases.length)
  try {
    const raw = localStorage.getItem(storageKey())
    if (!raw) return
    const list = JSON.parse(raw) as Partial<CaseEdit>[]
    for (const c of list) {
      suiteCases.push({
        name: c.name ?? '',
        gateway_tool: c.gateway_tool ?? '',
        arguments_text: c.arguments_text ?? '',
        expected_substring: c.expected_substring ?? '',
        timeout_ms: c.timeout_ms,
      })
    }
  } catch {
    /* ignore corrupt local data */
  }
}

function persistCases() {
  if (!serverId.value) return
  try {
    localStorage.setItem(storageKey(), JSON.stringify(suiteCases))
  } catch {
    /* storage full/disabled — non-fatal */
  }
}

function addCase() {
  suiteCases.push({ name: `case-${suiteCases.length + 1}`, gateway_tool: '', arguments_text: '', expected_substring: '' })
}
// ---- 质量评测 ----
async function runQuality() {
  if (!serverId.value) return
  qualityLoading.value = true
  try {
    report.value = await runEvalQuality(serverId.value, targetInstance.value || undefined)
    await loadMeta()
  } catch (e) {
    message.error(String(e))
  } finally {
    qualityLoading.value = false
  }
}

// ---- 回归执行 ----
async function runSuite() {
  if (!serverId.value || !suiteCases.length) return
  const parsed: { name: string; gateway_tool: string; arguments: Record<string, unknown>; expected_substring: string; timeout_ms?: number }[] = []
  for (const c of suiteCases) {
    if (!c.name || !c.gateway_tool) {
      message.warning(t('evaluation.caseRequired'))
      return
    }
    let args: Record<string, unknown> = {}
    if (c.arguments_text.trim()) {
      try {
        const v = JSON.parse(c.arguments_text)
        if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error('obj')
        args = v
      } catch {
        message.warning(`${c.name}: arguments 需为 JSON 对象`)
        return
      }
    }
    parsed.push({ name: c.name, gateway_tool: c.gateway_tool, arguments: args, expected_substring: c.expected_substring, timeout_ms: c.timeout_ms || undefined })
  }
  suiteLoading.value = true
  try {
    suiteResult.value = await runEvalSuite(serverId.value, parsed, targetInstance.value || undefined)
  } catch (e) {
    message.error(String(e))
  } finally {
    suiteLoading.value = false
  }
}

// ---- 导出 PDF：打印当前评测内容 ----
function exportPdf() {
  window.print()
}

// ---- 展示辅助 ----
const dimNameKey: Record<string, string> = {
  protocol_compat: 'protocolCompat',
  schema_quality: 'schemaQuality',
  tool_description: 'toolDescription',
  tool_naming: 'toolNaming',
  reliability: 'reliability',
  performance: 'performance',
}

function dimLabel(key: string): string {
  return t(`evaluation.${dimNameKey[key] ?? key}`)
}

const toolColumns = computed<any[]>(() => [
  { title: t('evaluation.gatewayTool'), key: 'gateway_name', dataIndex: 'gateway_name', ellipsis: true },
  { title: t('evaluation.originalTool'), key: 'tool', dataIndex: 'tool', width: 160 },
  { title: t('evaluation.schemaScore'), key: 'schema', width: 90 },
  { title: t('evaluation.descriptionScore'), key: 'description', width: 90 },
  { title: t('evaluation.namingScore'), key: 'naming', width: 90 },
  { title: t('evaluation.findings'), key: 'findings', width: 120 },
])

const caseColumns = computed<any[]>(() => [
  { title: t('evaluation.caseName'), key: 'name', dataIndex: 'name' },
  { title: t('evaluation.gatewayTool'), key: 'gateway_tool', dataIndex: 'gateway_tool', width: 180 },
  { title: t('evaluation.errorCode'), key: 'error_code', dataIndex: 'error_code', width: 130 },
  { title: t('evaluation.p95Latency'), key: 'latency', width: 100 },
  { title: t('evaluation.outputPreview'), key: 'output', ellipsis: true },
  { title: '', key: 'result', width: 90 },
])

// 按网关工具分组的回归汇总列。
const byToolColumns = computed<any[]>(() => [
  { title: t('evaluation.gatewayTool'), key: 'gateway_tool', dataIndex: 'gateway_tool' },
  { title: t('evaluation.total'), key: 'total', dataIndex: 'total', width: 90 },
  { title: t('evaluation.passCount'), key: 'passed', width: 120 },
  { title: t('evaluation.p95Latency'), key: 'p95', width: 120 },
])

function severityColor(sev: string) {
  return sev === 'error' ? 'red' : sev === 'warn' ? 'orange' : 'blue'
}

function scoreTag(v: number) {
  return v >= 80 ? 'green' : v >= 50 ? 'orange' : 'red'
}

function scoreColor(v: number) {
  return v >= 80 ? '#52c41a' : v >= 50 ? '#fa8c16' : '#f5222d'
}

function scoreText(v?: number) {
  return v === undefined || v === null ? '-' : `${Math.round(v)}`
}

function rateText(v: number) {
  return `${(v * 100).toFixed(0)}%`
}

// pctText 把 0..1 的比例显示为百分比（如维度权重）。
function pctText(v: number) {
  return `${Math.round((v ?? 0) * 100)}%`
}

function msText(v: number) {
  return v ? `${Math.round(v)}ms` : '-'
}

function formatTime(iso: string) {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleString()
}
</script>

<style scoped>
.mb {
  margin-bottom: 16px;
}
.meta-item {
  font-size: 13px;
  color: var(--mc-ink-2);
}
.report-head {
  display: flex;
  align-items: center;
  gap: 20px;
  margin-bottom: 4px;
}
.score-big {
  font-size: 56px;
  font-weight: 700;
  color: #1f6feb;
  line-height: 1;
}
.report-head-side {
  flex: 1;
}
.report-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--mc-ink);
}
.report-sub,
.runtime-note {
  font-size: 12px;
  color: var(--mc-ink-3);
  margin-top: 2px;
}
.dim-tile {
  border: 1px solid var(--mc-line);
  border-radius: 8px;
  padding: 10px 12px;
}
.dim-name {
  font-size: 12px;
  color: var(--mc-ink-2);
  margin-bottom: 6px;
}
.dim-score {
  font-size: 20px;
  font-weight: 600;
  margin-bottom: 4px;
}
.dim-na {
  color: var(--mc-ink-3);
}
.dim-note {
  font-size: 12px;
  color: var(--mc-ink-3);
}
.case-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 8px;
}
.case-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.case-name {
  flex: 0 0 160px;
}
.case-tool {
  flex: 0 0 180px;
}
.case-args {
  flex: 1;
  min-width: 160px;
}
.case-expect {
  flex: 0 0 200px;
}
.case-timeout {
  flex: 0 0 90px;
}
.dim-weight {
  font-size: 11px;
  color: var(--mc-ink-3);
  margin-top: 6px;
}
.runtime-stats {
  display: flex;
  gap: 24px;
  padding: 8px 0 2px;
}
.issues {
  margin-top: 8px;
}
.issue {
  display: flex;
  gap: 8px;
  align-items: baseline;
  font-size: 13px;
  padding: 2px 0;
}
.finding-list {
  margin: 0;
  padding-left: 0;
  list-style: none;
}
.finding-list li {
  padding: 3px 0;
  font-size: 13px;
}
.finding-tag {
  margin-right: 6px;
}
.finding-suggestion {
  color: var(--mc-ink-3);
  font-size: 12px;
  margin: 2px 0 0 8px;
}
.stat {
  font-size: 13px;
}
.stat b {
  margin-right: 4px;
  color: var(--mc-ink-2);
}
.stat.ok b {
  color: #52c41a;
}
.stat.bad b {
  color: #f5222d;
}
.snippet {
  font-size: 12px;
  word-break: break-all;
}
.muted {
  color: var(--mc-ink-3);
  font-size: 12px;
}
</style>

<!-- 打印样式（全局，仅打印时生效）：隐藏应用外壳/工具栏，内容页不滚动 -->
<style>
@media print {
  @page {
    size: A4;
    margin: 12mm;
  }
  html,
  body {
    background: #fff !important;
  }
  .layout .sider,
  .layout .header,
  .no-print {
    display: none !important;
  }
  .layout .content {
    overflow: visible !important;
    padding: 0 !important;
  }
  .report-area {
    break-inside: auto;
    box-shadow: none !important;
    border: none !important;
  }
  .ant-table {
    font-size: 11px;
  }
  .ant-tabs-nav,
  .ant-tabs-content-holder {
    margin: 0 !important;
  }
}
</style>
