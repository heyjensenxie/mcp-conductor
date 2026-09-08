<template>
  <div ref="el" class="chart"></div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { getChartTheme, THEME_CHANGE_EVENT } from '@/theme'

// 按需注册，避免整包 echarts 进入应用（与 TopToolsChart 一致）。
echarts.use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

// 流量趋势数据：按分钟聚合的调用量（totals）与成功率（rates，0-100）。
const props = defineProps<{
  categories: string[]
  totals: number[]
  rates: number[]
}>()

const el = ref<HTMLDivElement>()
let chart: ReturnType<typeof echarts.init> | null = null

function render() {
  if (!chart) return
  const palette = getChartTheme()
  chart.setOption({
    grid: { left: 8, right: 16, top: 32, bottom: 8, containLabel: true },
    tooltip: {
      trigger: 'axis',
      backgroundColor: palette.tooltipBackground,
      borderColor: palette.border,
      shadowColor: palette.tooltipShadow,
      textStyle: { color: palette.tooltipText, fontSize: 12 },
    },
    legend: { top: 0, right: 0, itemWidth: 12, itemHeight: 8, textStyle: { color: palette.label } },
    xAxis: {
      type: 'category',
      data: props.categories,
      boundaryGap: false,
      axisLine: { lineStyle: { color: palette.border } },
      axisLabel: { color: palette.axis },
    },
    yAxis: [
      { type: 'value', name: 'calls', minInterval: 1, axisLabel: { color: palette.axis }, splitLine: { lineStyle: { color: palette.grid, type: 'dashed' } } },
      { type: 'value', name: 'rate%', min: 0, max: 100, axisLabel: { color: palette.axis }, splitLine: { show: false } },
    ],
    series: [
      {
        name: 'calls',
        type: 'line',
        smooth: true,
        symbol: 'none',
        data: props.totals,
        lineStyle: { width: 2, color: palette.primary },
        itemStyle: { color: palette.primary },
        areaStyle: { color: palette.areaStart, opacity: 1 },
      },
      {
        name: 'success rate %',
        type: 'line',
        smooth: true,
        symbol: 'none',
        yAxisIndex: 1,
        data: props.rates,
        lineStyle: { width: 2, color: palette.success },
        itemStyle: { color: palette.success },
      },
    ],
  })
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
watch(() => [props.categories, props.totals, props.rates], render, { deep: true })
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  window.removeEventListener(THEME_CHANGE_EVENT, render)
  chart?.dispose()
  chart = null
})
</script>

<style scoped>
.chart {
  height: 260px;
  width: 100%;
}
</style>
