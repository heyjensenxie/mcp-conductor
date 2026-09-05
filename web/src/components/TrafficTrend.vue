<template>
  <div ref="el" class="chart"></div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

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
  chart.setOption({
    grid: { left: 8, right: 16, top: 32, bottom: 8, containLabel: true },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, right: 0, itemWidth: 12, itemHeight: 8 },
    xAxis: { type: 'category', data: props.categories, boundaryGap: false },
    yAxis: [
      { type: 'value', name: 'calls', minInterval: 1, splitLine: { lineStyle: { type: 'dashed' } } },
      { type: 'value', name: 'rate%', min: 0, max: 100, splitLine: { show: false } },
    ],
    series: [
      {
        name: 'calls',
        type: 'line',
        smooth: true,
        symbol: 'none',
        data: props.totals,
        lineStyle: { width: 2, color: '#1677ff' },
        itemStyle: { color: '#1677ff' },
        areaStyle: { opacity: 0.08 },
      },
      {
        name: 'success rate %',
        type: 'line',
        smooth: true,
        symbol: 'none',
        yAxisIndex: 1,
        data: props.rates,
        lineStyle: { width: 2, color: '#52c41a' },
        itemStyle: { color: '#52c41a' },
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
})
watch(() => [props.categories, props.totals, props.rates], render, { deep: true })
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
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
