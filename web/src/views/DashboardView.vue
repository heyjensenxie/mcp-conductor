<template>
  <div class="dash">
    <!-- 首屏加载 -->
    <div v-if="!booted" class="dash-boot">
      <a-spin size="large" />
      <span class="boot-text mono">{{ t('dashboard.loading') }}</span>
    </div>

    <template v-else>
      <div class="dash-topbar">
        <a-button size="small" type="text" class="topbar-refresh" :loading="refreshing" @click="manualRefresh">
          <template #icon><ReloadOutlined /></template>
          {{ t('common.refresh') }}
        </a-button>
      </div>

      <!-- 异常横幅：整页加载失败（鉴权/网络）时提示，不打断其余模块 -->
      <a-alert v-if="loadError" type="warning" show-icon class="row-gap" :message="t('dashboard.loadFailed')" :description="loadError" banner />

      <!-- ===== 顶部指标卡：围绕 6 个核心问题 ===== -->
      <a-row :gutter="[16, 16]" class="row-gap">
        <a-col v-for="(c, i) in statCards" :key="c.key" :xs="12" :lg="6" :xl="4" class="reveal" :style="{ animationDelay: `${i * 50}ms` }">
          <div class="stat-card mc-panel">
            <div class="stat-head">
              <span class="stat-label mono">{{ c.label }}</span>
              <span v-if="c.dot" class="mc-dot" :class="c.dot"></span>
            </div>
            <div class="stat-value mono">{{ c.value }}</div>
            <div class="stat-sub" :class="c.subTone ? `stat-sub--${c.subTone}` : ''">{{ c.sub }}</div>
          </div>
        </a-col>
      </a-row>

      <!-- ===== 第二行：最近调用 + Server 健康 ===== -->
      <a-row :gutter="[16, 16]" class="row-gap reveal">
        <a-col :xs="24" :xl="16">
          <section class="mc-panel panel">
            <header class="panel-head">
              {{ t('dashboard.recentCalls') }}
              <a class="hd-link" @click.prevent="go('/traffic')">{{ t('dashboard.viewAll') }}</a>
            </header>
            <a-table
              v-if="logs.length"
              :data-source="logs"
              :columns="recentColumns"
              size="small"
              :pagination="recentPagination"
              :row-key="(r: any) => r.id ?? r.request_id"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.key === 'tool'">
                  <a-tooltip :title="record.tool"><span class="cell-main cell-ellipsis mono" style="max-width: 100%">{{ record.tool }}</span></a-tooltip>
                </template>
                <template v-else-if="column.key === 'status'">
                  <a-tooltip :title="record.error || record.status">
                    <span class="mc-dot" :class="statusDot(record.status)"></span>
                    <span class="cell-status" :class="`cell-status--${statusDot(record.status).replace('mc-dot--', '')}`">{{ statusText(record.status) }}</span>
                  </a-tooltip>
                </template>
                <template v-else-if="column.key === 'latency'">
                  <span class="mono" :class="record.latency_ms > 1000 ? 'text-slow' : ''">{{ record.latency_ms }}<span class="unit">ms</span></span>
                </template>
                <template v-else-if="column.key === 'server'">
                  <a-tooltip :title="record.server_id">
                    <span class="cell-ellipsis mono">{{ serverName(record.server_id) }}</span>
                  </a-tooltip>
                </template>
                <template v-else-if="column.key === 'client'">
                  <span class="cell-ellipsis">{{ record.client || '-' }}</span>
                </template>
                <template v-else-if="column.key === 'request_id'">
                  <a-tooltip :title="t('dashboard.copyRequestId')">
                    <span class="rid mono" @click="copyText(record.request_id)">
                      {{ record.request_id }}
                    </span>
                  </a-tooltip>
                </template>
                <template v-else-if="column.key === 'time'">
                  <a-tooltip :title="fmtFull(record.timestamp)">
                    <span class="mono time-cell">{{ fmtTime(record.timestamp) }}</span>
                  </a-tooltip>
                </template>
              </template>
            </a-table>
            <div v-else class="empty">
              <a-empty :description="t('dashboard.noLogs')" :image-style="{ height: '56px' }" />
            </div>
          </section>
        </a-col>
        <a-col :xs="24" :xl="8">
          <section class="mc-panel panel">
            <header class="panel-head">
              {{ t('dashboard.serverHealth') }}
            </header>
            <template v-if="servers.length">
              <ul class="srv-list">
                <li v-for="row in serverRows" :key="row.s.id" class="srv-row">
                  <div class="srv-top">
                    <span class="mc-dot" :class="row.dot"></span>
                    <a class="srv-name mono" :title="row.s.id" @click.prevent="go(`/servers/${row.s.id}`)">{{ row.s.name }}</a>
                    <span v-if="!row.s.enabled" class="srv-tag">{{ t('dashboard.disabled') }}</span>
                    <span class="srv-status" :class="`cell-status--${row.dot.replace('mc-dot--', '')}`">{{ statusOfServer(row.s) }}</span>
                  </div>
                  <div class="srv-metrics">
                    <div class="srv-metric">
                      <span class="srv-k mono">{{ t('dashboard.metricTools') }}</span>
                      <span class="srv-v mono">{{ row.tools }}</span>
                    </div>
                    <div class="srv-metric">
                      <span class="srv-k mono">{{ t('dashboard.metricRequests') }}</span>
                      <span class="srv-v mono">{{ row.req }}</span>
                    </div>
                    <div class="srv-metric">
                      <span class="srv-k mono">{{ t('dashboard.metricSuccess') }}</span>
                      <span class="srv-v mono" :class="row.req && row.ok === row.req ? '' : 'text-slow'">{{ row.req ? `${row.success}%` : '—' }}</span>
                    </div>
                    <div class="srv-metric">
                      <span class="srv-k mono">{{ t('dashboard.metricAvg') }}</span>
                      <span class="srv-v mono">{{ row.avg === null ? '—' : `${row.avg}ms` }}</span>
                    </div>
                  </div>
                </li>
              </ul>
              <div class="srv-foot mono">{{ t('dashboard.lastCheck', { n: checkAgo }) }}</div>
            </template>
            <div v-else class="empty">
              <a-empty :description="t('dashboard.noServers')">
                <a-button type="primary" size="small" @click="go('/servers')">{{ t('dashboard.goServers') }}</a-button>
              </a-empty>
            </div>
          </section>
        </a-col>
      </a-row>

      <!-- ===== 调用趋势（可切换指标 + 时间范围） ===== -->
      <section class="mc-panel panel row-gap reveal">
        <header class="panel-head panel-head--toolbar">
          <span>{{ t('dashboard.callTrend') }}</span>
          <div class="hd-extra">
            <a-radio-group v-model:value="mode" size="small" button-style="solid" class="mode-switch">
              <a-radio-button value="requests">{{ t('dashboard.trendRequests') }}</a-radio-button>
              <a-radio-button value="success">{{ t('dashboard.trendSuccess') }}</a-radio-button>
              <a-radio-button value="latency">{{ t('dashboard.trendLatency') }}</a-radio-button>
            </a-radio-group>
            <a-select v-model:value="windowMinutes" size="small" style="width: 124px" :options="windowOptions" />
            <a-tooltip :title="t('dashboard.reloadTrend')">
              <a-button size="small" type="text" :loading="windowLoading" @click="loadWindow">
                <template #icon><ReloadOutlined /></template>
              </a-button>
            </a-tooltip>
          </div>
        </header>
        <div class="trend-body">
          <a-spin :spinning="windowLoading">
            <CallTrendChart
              v-if="activeTrend"
              :categories="activeTrend.categories"
              :series="activeTrend.series"
              :unit="activeTrend.unit"
              :y-min="activeTrend.yMin"
              :y-max="activeTrend.yMax"
              :height="190"
            />
            <div v-else class="empty empty--trend">
              <a-empty :description="t('dashboard.noTrend')" :image-style="{ height: '44px' }" />
            </div>
          </a-spin>
        </div>
      </section>

      <!-- ===== 第四行：Tool Analytics + Exception Overview（65 / 35） ===== -->
      <a-row :gutter="[16, 16]" class="row-gap reveal">
        <a-col :xs="24" :xl="16">
          <section class="mc-panel panel h-full">
            <header class="panel-head">
              {{ t('dashboard.toolAnalytics') }}
              <span class="hd-note mono">{{ t('dashboard.toolsTopN', { n: 6 }) }}</span>
            </header>
            <a-radio-group v-model:value="toolTab" size="small" button-style="solid" class="tab-row">
              <a-radio-button value="calls">{{ t('dashboard.tabByCalls') }}</a-radio-button>
              <a-radio-button value="slow">{{ t('dashboard.tabBySlow') }}</a-radio-button>
              <a-radio-button value="fail">{{ t('dashboard.tabByFail') }}</a-radio-button>
            </a-radio-group>

            <div class="tl-body">
              <!-- 调用量 TOP -->
              <ul v-if="toolTab === 'calls'" class="tl-list">
                <li v-if="callsTop.length" v-for="(row, i) in callsTop" :key="row.name" class="tl-row tl-row--calls">
                  <span class="tl-rank mono">{{ i + 1 }}</span>
                  <span class="tl-name mono" :title="row.name">{{ row.name }}</span>
                  <span class="calls-track"><i class="calls-fill" :style="{ width: `${(row.totals / callsMax) * 100}%` }"></i></span>
                  <span class="tl-val mono">{{ row.totals }}</span>
                </li>
                <li v-else class="empty tl-empty"><a-empty :description="t('dashboard.emptyToolData')" :image-style="{ height: '40px' }" /></li>
              </ul>

              <!-- 慢调用 TOP -->
              <div v-else-if="toolTab === 'slow'" class="tl-table-wrap">
                <div class="tl-cols mono">
                  <span>{{ t('traffic.tool') }}</span>
                  <span class="tl-col-r">{{ t('dashboard.colLatencyP95') }}</span>
                  <span class="tl-col-r">{{ t('dashboard.colLatencyP50') }}</span>
                </div>
                <ul class="tl-list">
                  <li v-if="slowTop.length" v-for="row in slowTop" :key="row.name" class="tl-row tl-row--grid">
                    <span class="tl-name mono" :title="row.name">{{ row.name }}</span>
                    <span class="tl-val tl-col-r mono">{{ row.p95 }}<span class="unit">ms</span></span>
                    <span class="tl-sub tl-col-r mono">{{ row.p50 }}<span class="unit">ms</span></span>
                  </li>
                  <li v-else class="empty tl-empty"><a-empty :description="t('dashboard.emptyToolData')" :image-style="{ height: '40px' }" /></li>
                </ul>
              </div>

              <!-- 失败率 TOP -->
              <div v-else class="tl-table-wrap">
                <div class="tl-cols mono">
                  <span>{{ t('traffic.tool') }}</span>
                  <span class="tl-col-r">{{ t('dashboard.colFailureRate') }}</span>
                  <span class="tl-col-r">{{ t('dashboard.colFailedTotal') }}</span>
                </div>
                <ul class="tl-list">
                  <li v-if="failTop.length" v-for="row in failTop" :key="row.name" class="tl-row tl-row--grid">
                    <span class="tl-name mono" :title="row.name">{{ row.name }}</span>
                    <span class="tl-val tl-col-r mono text-slow">{{ row.failure }}%</span>
                    <span class="tl-sub tl-col-r mono">{{ row.errors }} / {{ row.totals }}</span>
                  </li>
                  <li v-else class="empty tl-empty"><a-empty :description="t('dashboard.emptyFailRate')" :image-style="{ height: '40px' }" /></li>
                </ul>
              </div>
            </div>
          </section>
        </a-col>
        <a-col :xs="24" :xl="8">
          <section class="mc-panel panel h-full">
            <header class="panel-head">
              {{ t('dashboard.exceptionOverview') }}
              <span v-if="logs.length" class="hd-note mono">{{ t('dashboard.sampleBasis', { n: logs.length }) }}</span>
            </header>
            <div v-if="!logs.length" class="empty"><a-empty :description="t('dashboard.noLogs')" :image-style="{ height: '56px' }" /></div>
            <template v-else>
              <ul class="exc-list">
                <li v-for="c in excRows" :key="c.cat" class="exc-row">
                  <span class="mc-dot" :class="c.count ? excDot(c.cat) : 'mc-dot--idle'"></span>
                  <span class="exc-label">{{ c.label }}</span>
                  <span class="exc-count mono" :class="c.count ? excTone(c.cat) : ''">{{ c.count }}</span>
                </li>
              </ul>
              <div class="issues">
                <div class="issues-head mono">{{ t('dashboard.recentIssues') }}</div>
                <ul v-if="issues.length" class="issues-list">
                  <li v-for="it in issues" :key="`${it.request_id}-${it.id ?? 0}`" class="issue-row">
                    <span class="issue-dot" :class="`issue-dot--${it.tone}`"></span>
                    <div class="issue-main">
                      <a-tooltip :title="it.tool">
                        <div class="issue-tool mono cell-ellipsis">{{ it.tool }}</div>
                      </a-tooltip>
                      <div class="issue-msg" :title="it.detail">{{ it.detail }}</div>
                    </div>
                    <a-tooltip :title="it.request_id">
                      <span class="issue-time mono" @click="copyText(it.request_id)">{{ fmtClockOf(it.ts) }}</span>
                    </a-tooltip>
                  </li>
                </ul>
                <div v-else class="issue-none"><span class="mc-dot mc-dot--ok"></span>{{ t('dashboard.issueNone') }}</div>
              </div>
              <a v-if="issues.length" class="hd-link issues-more" @click.prevent="go('/observability')">{{ t('dashboard.viewAll') }}</a>
            </template>
          </section>
        </a-col>
      </a-row>

      <!-- ===== 多 Server 时：路由 / 流量分布 ===== -->
      <section v-if="serverRows.length > 1" class="mc-panel panel row-gap reveal">
        <header class="panel-head">
          {{ t('dashboard.routeDistribution') }}
          <span class="hd-note mono">{{ routeHealthy }}</span>
        </header>
        <ul class="route-list">
          <li v-for="row in routeRows" :key="row.s.id" class="route-row">
            <span class="mc-dot" :class="row.dot"></span>
            <a class="srv-name mono route-name" :title="row.s.id" @click.prevent="go(`/servers/${row.s.id}`)">{{ row.s.name }}</a>
            <span class="route-track"><i class="route-fill" :style="{ width: `${row.percent}%` }"></i></span>
            <span class="route-pct mono">{{ row.percent }}%</span>
            <span class="route-val mono">{{ row.req }}</span>
          </li>
        </ul>
        <div class="route-note mono">{{ t('dashboard.routeNote') }}</div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { getLogs, getMetricsTrend, listAllServers, listServerToolsAll } from '@/api'
import type { MCPServer, TrafficSample, TrendPoint } from '@/types'
import CallTrendChart, { type TrendSeries } from '@/components/CallTrendChart.vue'

// ---------- 类型与常量 ----------
type TrendMode = 'requests' | 'success' | 'latency'
type ToolTab = 'calls' | 'slow' | 'fail'
type LogCat = 'failed' | 'timeout' | 'limited' | 'denied' | 'unavailable'

const CAT_ORDER: LogCat[] = ['failed', 'timeout', 'limited', 'denied', 'unavailable']
// 日志 status 分类：success 之外按错误码归类；未列出的归为调用失败。
function catOf(status: string): LogCat {
  switch (status) {
    case 'timeout_error':
      return 'timeout'
    case 'rate_limit_error':
      return 'limited'
    case 'authorization_error':
    case 'authentication_error':
      return 'denied'
    case 'route_error':
      return 'unavailable'
    default:
      return 'failed'
  }
}
const EXC_TONE: Record<LogCat, 'bad' | 'warn'> = {
  failed: 'bad',
  timeout: 'warn',
  limited: 'warn',
  denied: 'bad',
  unavailable: 'warn',
}

const { t } = useI18n()
const router = useRouter()

// ---------- 原始数据 ----------
const servers = ref<MCPServer[]>([])
const toolCount = ref<Record<string, number>>({})
const logs = ref<TrafficSample[]>([])
const trendPoints = ref<TrendPoint[]>([]) // 窗口真时序（请求量 / 成功率）
const latencyPoints = ref<{ ts: number; avg: number; p95: number | null }[]>([])

const mode = ref<TrendMode>('requests')
const toolTab = ref<ToolTab>('calls')
const windowMinutes = ref(30)
const booted = ref(false)
const refreshing = ref(false)
const windowLoading = ref(false)
const loadError = ref('')
const lastCheck = ref(0)
const nowTick = ref(Date.now())
let suspendWindow = false // auto-fit 期间抑制 window watcher 重复请求
let fitted = false
let pollTimer: number | undefined
let tickTimer: number | undefined

// ---------- 基础工具 ----------
function fmtAxis(ts: number): string {
  const d = new Date(ts * 1000)
  const hh = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  if (windowMinutes.value >= 6 * 60) return `${d.getMonth() + 1}-${d.getDate()} ${hh}`
  return hh
}
function fmtTime(iso: string): string {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? '—' : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
function fmtFull(iso: string): string {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleString()
}
function fmtClockOf(ts: number): string {
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
function pct(v: number): number {
  return Math.round(v)
}
function fmtInt(n: number): string {
  return n.toLocaleString()
}
function fmtRate(v: number): string {
  if (v >= 100) return Math.round(v).toString()
  if (v >= 1) return v.toFixed(1)
  return (Math.round(v * 100) / 100).toString()
}
function percentile(sorted: number[], p: number): number | null {
  if (!sorted.length) return null
  const pos = p * (sorted.length - 1)
  const lo = Math.floor(pos)
  const hi = Math.ceil(pos)
  if (lo === hi) return sorted[lo]
  const f = pos - lo
  return sorted[lo] * (1 - f) + sorted[hi] * f
}
function serverName(id: string): string {
  return servers.value.find((s) => s.id === id)?.name || id
}
async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('access.copied'))
  } catch {
    message.warning(t('access.copyFailed'))
  }
}
function go(path: string) {
  void router.push(path)
}

// ---------- 状态行短词 / 色 ----------
const statusText = (s: string) =>
  s === 'success'
    ? t('traffic.statusSuccess')
    : s === 'timeout_error'
      ? 'timeout'
      : s === 'rate_limit_error'
        ? 'rate_limited'
        : s === 'authorization_error' || s === 'authentication_error'
          ? 'denied'
          : s === 'route_error'
            ? 'unavailable'
            : 'failed'
const statusDot = (s: string) => (s === 'success' ? 'mc-dot--ok' : EXC_TONE[catOf(s)] === 'warn' ? 'mc-dot--warn' : 'mc-dot--bad')
const statusOfServer = (s: MCPServer): string => {
  if (!s.enabled) return t('dashboard.disabled')
  if (s.health_status === 'healthy') return t('dashboard.healthy')
  if (s.health_status === 'unhealthy') return t('dashboard.unhealthy')
  if (s.health_status === 'disabled') return t('dashboard.disabled')
  return t('dashboard.unknown')
}
const srvDot = (s: MCPServer): string => {
  if (!s.enabled) return 'mc-dot--idle'
  if (s.health_status === 'healthy') return 'mc-dot--ok'
  if (s.health_status === 'unhealthy') return 'mc-dot--bad'
  return 'mc-dot--warn'
}

// ---------- 统计卡（6 个核心问题） ----------
interface StatCard {
  key: string
  label: string
  value: string | number
  sub: string
  subTone?: 'warn' | ''
  dot: string
}
const healthyCount = computed(() => servers.value.filter((s) => s.enabled && s.health_status === 'healthy').length)
const totalTools = computed(() => Object.values(toolCount.value).reduce((a, b) => a + b, 0))
// 顶部「流量」一族（Requests / Success / Exceptions / 延迟）统一以窗口内日志样本为口径，
// 与 Recent Calls 表格同源，避免“成功率 X% 但异常 0”这类跨口径矛盾。
const sampleOk = computed(() => logs.value.filter((l) => l.status === 'success').length)

// 最近日志样本上的分位延迟（与下方表格同源，口径一致）
const latSorted = computed(() =>
  logs.value
    .map((l) => l.latency_ms)
    .filter((v) => typeof v === 'number')
    .sort((a, b) => a - b),
)
const p50ms = computed(() => percentile(latSorted.value, 0.5))
const p95ms = computed(() => percentile(latSorted.value, 0.95))
const p99ms = computed(() => percentile(latSorted.value, 0.99))

// 异常计数（来自最近日志样本；分类能给出 timeout / rate-limit / denied 等语义）
const anomalies = computed(() => logs.value.filter((l) => l.status !== 'success'))
const excCount = computed(() => CAT_ORDER.map((c) => ({ cat: c, count: anomalies.value.filter((l) => catOf(l.status) === c).length })))
const excTotal = computed(() => excCount.value.reduce((s, r) => s + r.count, 0))

const statCards = computed<StatCard[]>(() => {
  const nServer = servers.value.length
  const serverValue = nServer ? `${healthyCount.value} / ${nServer}` : '—'
  const serverSub = !nServer
    ? t('dashboard.noServers')
    : healthyCount.value === nServer
      ? t('dashboard.serverAllHealthy')
      : t('dashboard.serverUnhealthy', { bad: nServer - healthyCount.value })
  const serverDot = !nServer ? '' : healthyCount.value === nServer ? 'mc-dot--ok' : nServer - healthyCount.value === nServer ? 'mc-dot--bad' : 'mc-dot--warn'

  const nCalls = logs.value.length
  const okCalls = sampleOk.value
  const failCalls = nCalls - okCalls
  const rate = windowMinutes.value ? nCalls / windowMinutes.value : 0
  const succPct = nCalls ? pct((okCalls / nCalls) * 100) : null

  // Exceptions 卡辅助：全零给出“失败 0 · 超时 0 · 限流 0”；有异常则列出非零分类。
  let excSub: string
  let excToneForSub: StatCard['subTone'] = ''
  if (!logs.value.length) {
    excSub = t('dashboard.noLogs')
  } else if (excTotal.value === 0) {
    excSub = t('dashboard.exceptionsZero')
  } else {
    excSub = excCount.value
      .filter((r) => r.count > 0)
      .map((r) => `${shortLabel(r.cat)} ${r.count}`)
      .join(' · ')
    excToneForSub = 'warn'
  }

  return [
    {
      key: 'server',
      label: t('dashboard.server'),
      value: serverValue,
      sub: serverSub,
      dot: serverDot,
    },
    {
      key: 'tools',
      label: t('dashboard.availableTools'),
      value: fmtInt(totalTools.value),
      sub: t('dashboard.toolsFromServers', { n: nServer }),
      dot: '',
    },
    {
      key: 'requests',
      label: t('dashboard.requests'),
      value: nCalls ? fmtInt(nCalls) : '—',
      sub: logs.value.length ? t('dashboard.throughput', { rate: fmtRate(rate) }) : t('dashboard.noLogs'),
      dot: '',
    },
    {
      key: 'success',
      label: t('dashboard.successRate'),
      value: succPct === null ? '—' : `${succPct}%`,
      sub: nCalls ? t('dashboard.successSplit', { ok: fmtInt(okCalls), fail: fmtInt(failCalls) }) : t('dashboard.noLogs'),
      subTone: succPct !== null && succPct < 100 ? 'warn' : '',
      dot: '',
    },
    {
      key: 'p95',
      label: t('dashboard.p95Latency'),
      value: p95ms.value === null ? '—' : `${pct(p95ms.value)}ms`,
      sub: p50ms.value === null ? '' : t('dashboard.p50p99', { p50: pct(p50ms.value), p99: pct(p99ms.value ?? p95ms.value ?? 0) }),
      subTone: p95ms.value !== null && p95ms.value > 1000 ? 'warn' : '',
      dot: '',
    },
    {
      key: 'exceptions',
      label: t('dashboard.exceptions'),
      value: logs.value.length ? excTotal.value : '—',
      sub: excSub,
      subTone: excToneForSub,
      dot: logs.value.length && excTotal.value ? (excDotAnyBad() ? 'mc-dot--bad' : 'mc-dot--warn') : '',
    },
  ]
})
function shortLabel(c: LogCat): string {
  return t(`dashboard.${c}Short`)
}
function excDotAnyBad(): boolean {
  return excCount.value.some((r) => r.count > 0 && EXC_TONE[r.cat] === 'bad')
}

// ---------- 最近调用列 ----------
const recentColumns = computed<any[]>(() => [
  { title: t('traffic.tool'), key: 'tool', dataIndex: 'tool', ellipsis: true },
  { title: t('traffic.status'), key: 'status', dataIndex: 'status', width: 108 },
  { title: t('traffic.latencyMs'), key: 'latency', dataIndex: 'latency_ms', width: 92, align: 'right' },
  { title: t('traffic.server'), key: 'server', dataIndex: 'server_id', width: 120 },
  { title: t('traffic.client'), key: 'client', dataIndex: 'client', width: 92, ellipsis: true },
  { title: t('traffic.requestId'), key: 'request_id', dataIndex: 'request_id', width: 170 },
  { title: t('traffic.time'), key: 'time', dataIndex: 'timestamp', width: 120 },
])
const recentPagination = computed(() =>
  logs.value.length > 8
    ? { pageSize: 8, showSizeChanger: false, showTotal: (n: number) => t('filter.total', { total: n }), size: 'small' as const }
    : false,
)

// ---------- Server 健康 ----------
const serverRows = computed(() =>
  servers.value.map((s) => {
    const sLogs = logs.value.filter((l) => l.server_id === s.id)
    const req = sLogs.length
    const ok = sLogs.filter((l) => l.status === 'success').length
    const lats = sLogs.map((l) => l.latency_ms)
    const avg = lats.length ? Math.round(lats.reduce((a, b) => a + b, 0) / lats.length) : null
    return {
      s,
      dot: srvDot(s),
      tools: toolCount.value[s.id] ?? 0,
      req,
      ok,
      success: req ? pct((ok / req) * 100) : 0,
      avg,
    }
  }),
)

// ---------- 调用趋势（分模式） ----------
const windowOptions = computed(() => [
  { value: 30, label: t('observability.window30m') },
  { value: 60, label: t('observability.window1h') },
  { value: 360, label: t('observability.window6h') },
  { value: 1440, label: t('observability.window1d') },
])

function bucketLatency(list: TrafficSample[]): { ts: number; avg: number; p95: number | null }[] {
  const byMin = new Map<number, number[]>()
  for (const l of list) {
    const ms = Date.parse(l.timestamp)
    if (isNaN(ms)) continue
    const min = Math.floor(ms / 60000)
    const arr = byMin.get(min) ?? []
    arr.push(l.latency_ms)
    byMin.set(min, arr)
  }
  return Array.from(byMin.entries())
    .sort((a, b) => a[0] - b[0])
    .map(([ts, vals]) => {
      const sorted = [...vals].sort((a, b) => a - b)
      const avg = Math.round(sorted.reduce((a, b) => a + b, 0) / sorted.length)
      return { ts: ts * 60000, avg, p95: sorted.length >= 3 ? Math.round(percentile(sorted, 0.95)!) : null }
    })
}

const activeTrend = computed<{ categories: string[]; series: TrendSeries[]; unit: string; yMin: number; yMax?: number } | null>(() => {
  if (mode.value === 'latency') {
    if (!latencyPoints.value.length) return null
    return {
      categories: latencyPoints.value.map((p) => fmtAxis(p.ts / 1000)),
      unit: 'ms',
      yMin: 0,
      series: [
        { name: t('dashboard.latencyAvg'), color: '#1f6feb', data: latencyPoints.value.map((p) => p.avg) },
        { name: t('dashboard.latencyP95'), color: '#7a8aa0', dashed: true, data: latencyPoints.value.map((p) => p.p95) },
      ],
    }
  }
  if (!trendPoints.value.some((p) => p.totals > 0)) return null
  const categories = trendPoints.value.map((p) => fmtAxis(p.ts))
  if (mode.value === 'success') {
    return {
      categories,
      unit: '%',
      yMin: 0,
      yMax: 100,
      series: [
        {
          name: t('dashboard.trendSuccess'),
          color: '#12a150',
          data: trendPoints.value.map((p) => (p.totals ? pct(((p.totals - p.errors) / p.totals) * 100) : null)),
        },
      ],
    }
  }
  return {
    categories,
    unit: '',
    yMin: 0,
    series: [{ name: t('dashboard.trendRequests'), color: '#1f6feb', data: trendPoints.value.map((p) => p.totals) }],
  }
})

// ---------- Tool Analytics ----------
// 与顶部流量卡片同源：按“当前时间窗口内日志样本”现场聚合各工具的调用量 / 分位延迟 / 失败率。
// 不复用进程内 /api/metrics 累计快照（内存态，容器重启即清零），保证与最近调用、异常等
// 区块口径一致且重启后可回溯（traffic_log 落库）。
const toolStats = computed(() => {
  const byTool = new Map<string, TrafficSample[]>()
  for (const l of logs.value) {
    const arr = byTool.get(l.tool)
    if (arr) arr.push(l)
    else byTool.set(l.tool, [l])
  }
  return Array.from(byTool.entries()).map(([name, rows]) => {
    const totals = rows.length
    const errors = rows.filter((r) => r.status !== 'success').length
    const lats = rows.map((r) => r.latency_ms).sort((a, b) => a - b)
    const p50 = lats.length ? Math.round(percentile(lats, 0.5)!) : 0
    const p95 = lats.length ? Math.round(percentile(lats, 0.95)!) : 0
    return { name, totals, errors, p50, p95, failure: totals ? Math.round((errors / totals) * 100) : 0 }
  })
})
const callsTop = computed(() => toolStats.value.slice().sort((a, b) => b.totals - a.totals).slice(0, 6))
const callsMax = computed(() => callsTop.value[0]?.totals ?? 1)
const slowTop = computed(() =>
  toolStats.value
    .filter((s) => s.totals > 0)
    .sort((a, b) => b.p95 - a.p95 || b.totals - a.totals)
    .slice(0, 6)
    .map((s) => ({ name: s.name, p95: s.p95, p50: s.p50 })),
)
const failTop = computed(() =>
  toolStats.value
    .filter((s) => s.errors > 0)
    .sort((a, b) => b.failure - a.failure || b.totals - a.totals)
    .slice(0, 6)
    .map((s) => ({ name: s.name, errors: s.errors, totals: s.totals, failure: s.failure })),
)

// ---------- Exception Overview ----------
const excRows = computed(() =>
  CAT_ORDER.map((c) => ({
    cat: c,
    label: t(`dashboard.cat${c.charAt(0).toUpperCase()}${c.slice(1)}`),
    count: excCount.value.find((r) => r.cat === c)?.count ?? 0,
  })),
)
const excDot = (c: LogCat) => (EXC_TONE[c] === 'warn' ? 'mc-dot--warn' : 'mc-dot--bad')
const excTone = (c: LogCat) => (EXC_TONE[c] === 'warn' ? 'text-warn' : 'text-bad')
const issues = computed(() =>
  anomalies.value.slice(0, 3).map((l) => ({
    request_id: l.request_id,
    id: l.id,
    tool: l.tool,
    ts: Date.parse(l.timestamp),
    tone: EXC_TONE[catOf(l.status)],
    detail: `${l.error || statusText(l.status)} · ${fmtTime(l.timestamp)}`,
  })),
)

// ---------- Route Distribution ----------
const routeHealthy = computed(() => t('dashboard.routeHealthy', { ok: healthyCount.value, total: servers.value.length }))
const routeRows = computed(() => {
  const rows = serverRows.value
  // 请求归属与顶部流量卡片同源（窗口内日志），避免同一区块出现两套口径。
  const total = rows.reduce((s, r) => s + r.req, 0) || 1
  return rows.map((r) => ({
    s: r.s,
    dot: r.dot,
    req: r.req,
    percent: Math.round((r.req / total) * 100),
  }))
})

// ---------- 数据加载 ----------
async function loadSummary() {
  const sl = await listAllServers()
  servers.value = sl
  const counts: Record<string, number> = {}
  await Promise.all(
    sl.map(async (s) => {
      try {
        counts[s.id] = (await listServerToolsAll(s.id)).length
      } catch {
        counts[s.id] = 0
      }
    }),
  )
  toolCount.value = counts
}

async function loadWindow() {
  const minutes = windowMinutes.value
  windowLoading.value = true
  try {
    const from = new Date(Date.now() - minutes * 60000).toISOString()
    const to = new Date().toISOString()
    const [trendRes, logRes] = await Promise.all([getMetricsTrend('tool', minutes), getLogs({ page: 1, page_size: 200, from, to })])
    trendPoints.value = trendRes.series
    // Recent Calls / 顶部卡片 / Server 健康 / 异常概览统一基于窗口内日志样本。
    logs.value = logRes.items
    if (mode.value === 'latency') {
      latencyPoints.value = bucketLatency(logRes.items)
    }
  } catch (e) {
    loadError.value = String(e)
  } finally {
    windowLoading.value = false
  }
}

async function refreshAll() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await Promise.all([loadSummary(), loadWindow()])
    loadError.value = ''
  } catch (e) {
    loadError.value = String(e)
  } finally {
    refreshing.value = false
    lastCheck.value = Date.now()
  }
}

async function boot() {
  await refreshAll()
  booted.value = true
  lastCheck.value = Date.now()
  // 数据在默认窗口（30 分钟）之外但系统确有调用时，自动放宽到能覆盖它的档位，
  // 避免首屏趋势显示为“无数据”而下方列表却有调用。
  if (!fitted && !trendPoints.value.some((p) => p.totals > 0)) {
    suspendWindow = true
    for (const mins of [60, 360, 1440]) {
      windowMinutes.value = mins
      await loadWindow()
      if (trendPoints.value.some((p) => p.totals > 0)) break
    }
    suspendWindow = false
    fitted = true
  }
  fitted = true
  schedulePoll()
}

function manualRefresh() {
  void refreshAll()
}
function schedulePoll() {
  clearTimeout(pollTimer)
  pollTimer = window.setTimeout(() => {
    void refreshAll().finally(schedulePoll)
  }, 30000)
}

// 切换指标或时间范围 → 重拉趋势（latency 模式会额外拉窗口日志做分桶）。
watch([mode, windowMinutes], () => {
  if (suspendWindow) return
  void loadWindow()
})

const checkAgo = computed(() => {
  void nowTick.value
  return Math.max(0, Math.round((nowTick.value - lastCheck.value) / 1000))
})

onMounted(() => {
  tickTimer = window.setInterval(() => {
    nowTick.value = Date.now()
  }, 1000)
  void boot()
})
onBeforeUnmount(() => {
  clearTimeout(pollTimer)
  clearInterval(tickTimer)
})
</script>

<style scoped>
.dash {
  max-width: 1720px;
  margin: 0 auto;
}
.row-gap {
  margin-top: 16px;
}
.dash-boot {
  min-height: 420px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  flex-direction: column;
}
.boot-text {
  font-size: 12px;
  color: var(--mc-ink-3);
  letter-spacing: 0.05em;
}

/* 顶栏：右对齐刷新，保持轻量 */
.dash-topbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 4px;
}
.topbar-refresh {
  color: var(--mc-ink-3);
}

/* ===== 顶部统计卡 ===== */
.stat-card {
  position: relative;
  padding: 12px 16px 14px;
  overflow: hidden;
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}
.stat-card:hover {
  transform: translateY(-1px);
  box-shadow: var(--mc-shadow-raise);
  border-color: var(--mc-line);
}
.stat-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 14px;
}
.stat-label {
  font-size: 11px;
  letter-spacing: 0.08em;
  color: var(--mc-ink-3);
  text-transform: uppercase;
}
.stat-value {
  font-size: 25px;
  font-weight: 650;
  line-height: 1.25;
  margin-top: 4px;
  color: var(--mc-ink);
  letter-spacing: -0.01em;
}
.stat-sub {
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--mc-ink-3);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.stat-sub--warn {
  color: var(--mc-warn);
}

/* ===== 面板头 ===== */
.panel {
  background: var(--mc-elev);
  padding: 14px 18px 16px;
}
.h-full {
  display: flex;
  flex-direction: column;
}
.panel-head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--mc-ink);
  margin-bottom: 12px;
}
.panel-head::before {
  content: '';
  width: 8px;
  height: 8px;
  border-radius: 2px;
  background: var(--mc-accent);
}
.hd-link {
  margin-left: auto;
  font-weight: 400;
  font-size: 12px;
  color: var(--mc-accent);
  cursor: pointer;
}
.hd-note {
  margin-left: auto;
  font-size: 11px;
  color: var(--mc-ink-3);
}
.hd-extra {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* ===== 表格通用 ===== */
.cell-main {
  color: var(--mc-ink);
}
.cell-ellipsis {
  display: inline-block;
  max-width: 100%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  vertical-align: bottom;
}
.cell-status {
  margin-left: 7px;
  font-size: 12px;
  color: var(--mc-ink-2);
}
.cell-status--ok {
  color: var(--mc-ok);
}
.cell-status--warn {
  color: var(--mc-warn);
}
.cell-status--bad {
  color: var(--mc-danger);
}
.cell-status--idle {
  color: var(--mc-ink-3);
}
.text-slow {
  color: var(--mc-warn) !important;
}
.text-bad {
  color: var(--mc-danger) !important;
}
.text-warn {
  color: var(--mc-warn) !important;
}
.time-cell {
  color: var(--mc-ink-3);
  font-size: 12px;
}
.unit {
  font-size: 10.5px;
  color: var(--mc-ink-3);
  margin-left: 1px;
}
.rid {
  display: inline-block;
  max-width: 100%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  vertical-align: bottom;
  color: var(--mc-ink-2);
  cursor: copy;
  border-bottom: 1px dashed transparent;
  font-size: 12px;
}
.rid:hover {
  color: var(--mc-accent);
  border-bottom-color: var(--mc-accent);
}

/* ===== Server 健康（紧凑状态行） ===== */
.srv-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.srv-row {
  border: 1px solid var(--mc-line-soft);
  border-radius: 8px;
  padding: 10px 12px;
  transition: border-color 0.18s ease, box-shadow 0.18s ease;
}
.srv-row:hover {
  border-color: var(--mc-line);
  box-shadow: var(--mc-shadow-raise);
}
.srv-top {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.srv-name {
  font-size: 13px;
  color: var(--mc-ink);
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 58%;
}
.srv-name:hover {
  color: var(--mc-accent);
}
.srv-tag {
  font-size: 10.5px;
  color: var(--mc-ink-3);
  border: 1px solid var(--mc-line);
  border-radius: 4px;
  padding: 0 5px;
  line-height: 16px;
  white-space: nowrap;
}
.srv-status {
  margin-left: auto;
  font-size: 11.5px;
  white-space: nowrap;
}
.srv-metrics {
  display: flex;
  gap: 6px;
  margin-top: 10px;
}
.srv-metric {
  flex: 1 1 0;
  background: var(--mc-bg);
  border-radius: 6px;
  padding: 6px 8px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.srv-k {
  font-size: 10px;
  color: var(--mc-ink-3);
  letter-spacing: 0.05em;
}
.srv-v {
  font-size: 14px;
  font-weight: 600;
  color: var(--mc-ink);
  white-space: nowrap;
}
.srv-foot {
  margin-top: 12px;
  text-align: right;
  font-size: 11px;
  color: var(--mc-ink-3);
}

/* ===== 趋势图 ===== */
.trend-body {
  min-height: 204px;
  display: flex;
  align-items: stretch;
}
.mode-switch {
  flex-wrap: nowrap;
}
.trend-body :deep(.ant-spin-nested-loading) {
  width: 100%;
}
.trend-body :deep(.ant-spin-container) {
  width: 100%;
}
.empty--trend {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
}

/* ===== Tool Analytics ===== */
.tab-row {
  margin-bottom: 10px;
}
.tl-body {
  min-height: 210px;
}
.tl-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.tl-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 2px;
}
.tl-rank {
  width: 18px;
  text-align: right;
  color: var(--mc-ink-3);
  font-size: 12px;
}
.tl-name {
  max-width: 46%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--mc-ink);
  font-size: 12.5px;
}
.calls-track {
  flex: 1;
  height: 7px;
  background: var(--mc-line-soft);
  border-radius: 4px;
  overflow: hidden;
  min-width: 40px;
}
.calls-fill {
  display: block;
  height: 100%;
  border-radius: 4px;
  background: var(--mc-accent);
  opacity: 0.9;
  transition: width 0.25s ease;
}
.tl-val {
  color: var(--mc-ink);
  font-size: 12.5px;
  font-weight: 600;
  min-width: 46px;
  text-align: right;
}
.tl-sub {
  color: var(--mc-ink-3);
  font-size: 12px;
}
.tl-col-r {
  text-align: right;
}
.tl-cols {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 104px 118px;
  gap: 10px;
  font-size: 11px;
  color: var(--mc-ink-3);
  padding: 2px 2px 6px;
  border-bottom: 1px solid var(--mc-line-soft);
  letter-spacing: 0.04em;
}
.tl-row--grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 104px 118px;
  gap: 10px;
}
.tl-row--grid .tl-name {
  max-width: none;
}
.tl-empty {
  padding: 30px 0;
  display: flex;
  justify-content: center;
}

/* ===== Exception Overview ===== */
.exc-list {
  list-style: none;
  margin: 0 0 4px;
  padding: 0;
  display: flex;
  flex-direction: column;
}
.exc-row {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 7px 0;
  border-bottom: 1px dashed var(--mc-line-soft);
}
.exc-label {
  font-size: 12.5px;
  color: var(--mc-ink-2);
}
.exc-count {
  margin-left: auto;
  font-size: 13px;
  font-weight: 600;
  color: var(--mc-ink-3);
}
.issues {
  margin-top: 14px;
}
.issues-head {
  font-size: 11px;
  color: var(--mc-ink-3);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  margin-bottom: 8px;
}
.issues-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.issue-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 8px 10px;
  border: 1px solid var(--mc-line-soft);
  border-radius: 8px;
  background: var(--mc-bg);
  min-width: 0;
}
.issue-dot {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-top: 5px;
}
.issue-dot--bad {
  background: var(--mc-danger);
}
.issue-dot--warn {
  background: var(--mc-warn);
}
.issue-main {
  flex: 1;
  min-width: 0;
}
.issue-tool {
  font-size: 12px;
  color: var(--mc-ink);
}
.issue-msg {
  font-size: 11.5px;
  color: var(--mc-ink-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-top: 2px;
}
.issue-time {
  flex: none;
  font-size: 11px;
  color: var(--mc-ink-3);
  cursor: copy;
  margin-top: 4px;
}
.issue-time:hover {
  color: var(--mc-accent);
}
.issue-none {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  color: var(--mc-ink-2);
  padding: 10px 2px;
}
.issues-more {
  display: inline-flex;
  margin-top: 10px;
}

/* ===== Route Distribution ===== */
.route-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.route-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.route-name {
  width: 200px;
  max-width: 34%;
  flex: none;
}
.route-track {
  flex: 1;
  height: 8px;
  background: var(--mc-line-soft);
  border-radius: 5px;
  overflow: hidden;
  min-width: 60px;
}
.route-fill {
  display: block;
  height: 100%;
  border-radius: 5px;
  background: linear-gradient(90deg, #1f6feb, #6ea2f2);
  transition: width 0.25s ease;
}
.route-pct {
  width: 52px;
  text-align: right;
  font-weight: 600;
  color: var(--mc-ink);
  font-size: 12.5px;
}
.route-val {
  width: 60px;
  text-align: right;
  font-size: 12px;
  color: var(--mc-ink-3);
}
.route-note {
  margin-top: 12px;
  font-size: 11px;
  color: var(--mc-ink-3);
  text-align: right;
}
.empty {
  padding: 18px 4px;
  display: flex;
  justify-content: center;
}
</style>
