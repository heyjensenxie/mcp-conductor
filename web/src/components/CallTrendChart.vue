<template>
  <div ref="el" class="chart" :style="{ height: `${props.height}px` }"></div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { getChartTheme, THEME_CHANGE_EVENT } from '@/theme'

// 按需注册，避免整包 echarts 进入应用（与 TrafficTrend / TopToolsChart 一致）。
echarts.use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

// Dashboard 调用趋势的轻量折线容器：支持单/双系列与空位（null = 断点）。
// 同一时刻只画一种语义（请求量 / 成功率 / 延迟），避免多轴与跨量纲混排。
export interface TrendSeries {
  name: string
  color: string
  data: (number | null)[]
  dashed?: boolean
}

const props = withDefaults(
  defineProps<{
    categories: string[]
    series: TrendSeries[]
    unit?: string // 追加到数值后的单位（如 ms / %），无则省略
    yMin?: number
    yMax?: number
    height?: number
  }>(),
  { unit: '', yMin: undefined, yMax: undefined, height: 190 },
)

const el = ref<HTMLDivElement>()
let chart: ReturnType<typeof echarts.init> | null = null

function render() {
  if (!chart) return
  const palette = getChartTheme()
  const unit = props.unit ? ` ${props.unit}` : ''
  const yMin = props.yMin
  const yMax = props.yMax
  const total = props.categories.length
  const seriesOpt = props.series.map((s) => {
    // 稀疏数据（如放宽窗口后整段只有个别分钟有调用）用折线 + null 断点会画不出
    // 孤立点：非空点占比低于阈值时补圆点，保证“有点可看”，密集时仍走纯净折线。
    const nonNull: number = s.data.reduce((n: number, v) => n + (v === null || v === undefined ? 0 : 1), 0)
    const sparse = nonNull > 0 && nonNull <= Math.max(2, Math.round(total * 0.2))
    return {
      name: s.name,
      type: 'line',
      smooth: true,
      connectNulls: false,
      data: s.data,
      symbol: sparse ? 'circle' : 'none',
      symbolSize: sparse ? 5 : 0,
      lineStyle: { width: 2, color: s.color, type: s.dashed ? 'dashed' : 'solid' },
      itemStyle: { color: s.color },
      emphasis: { lineStyle: { width: 2.6 } },
    }
  })
  chart.setOption(
    {
      animationDuration: 220,
      grid: { left: 8, right: 14, top: 22, bottom: 6, containLabel: true },
      tooltip: {
        trigger: 'axis',
        confine: true,
        axisPointer: { type: 'line', lineStyle: { color: palette.primary, type: 'dashed' } },
        backgroundColor: palette.tooltipBackground,
        borderColor: palette.border,
        shadowBlur: 24,
        shadowColor: palette.tooltipShadow,
        textStyle: { color: palette.tooltipText, fontSize: 12 },
        // 空位（null）显示为破折号，避免把断点误读成 0。
        formatter: (params: any[]) => {
          if (!params || !params.length) return ''
          const head = `<div style="font-weight:600;margin-bottom:4px;color:${palette.label}">${params[0].axisValue}</div>`
          const rows = params
            .map((p) => {
              const v = p.value === null || p.value === undefined ? '—' : `${p.value}${unit}`
              return `<div style="display:flex;align-items:center;gap:6px;line-height:1.7"><span style="display:inline-block;width:8px;height:2px;border-radius:1px;background:${p.color}"></span><span style="color:${palette.label}">${p.seriesName}</span><span style="margin-left:auto;font-variant-numeric:tabular-nums;font-weight:600;color:${palette.tooltipText}">${v}</span></div>`
            })
            .join('')
          return head + rows
        },
      },
      xAxis: {
        type: 'category',
        data: props.categories,
        boundaryGap: false,
        axisTick: { show: false },
        axisLine: { lineStyle: { color: palette.border } },
        axisLabel: { color: palette.axis, fontSize: 10.5, margin: 8 },
      },
      yAxis: {
        type: 'value',
        ...(yMin === undefined ? {} : { min: yMin }),
        ...(yMax === undefined ? {} : { max: yMax }),
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: palette.axis, fontSize: 10.5 },
        splitLine: { lineStyle: { color: palette.grid, type: 'dashed' } },
      },
      series: seriesOpt,
    },
    { notMerge: true }, // 整体替换，避免不同系列数之间切换时残留旧 series（延迟 avg+p95 → 成功率等）
  )
}

function onResize() {
  chart?.resize()
}

onMounted(() => {
  chart = echarts.init(el.value!)
  render()
  window.addEventListener('resize', onResize)
  window.addEventListener(THEME_CHANGE_EVENT, render)
})
watch(() => [props.categories, props.series, props.unit, props.yMin, props.yMax], render, { deep: true })
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  window.removeEventListener(THEME_CHANGE_EVENT, render)
  chart?.dispose()
  chart = null
})
</script>

<style scoped>
.chart {
  width: 100%;
}
</style>
