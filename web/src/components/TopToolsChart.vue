<template>
  <div ref="el" class="chart"></div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { getChartTheme, THEME_CHANGE_EVENT } from '@/theme'

// 按需注册，避免整包 echarts 进入应用（大体积）。
echarts.use([BarChart, GridComponent, TooltipComponent, CanvasRenderer])

export interface ChartDatum {
  name: string
  value: number
}

const props = defineProps<{ data: ChartDatum[] }>()

const el = ref<HTMLDivElement>()
let chart: ReturnType<typeof echarts.init> | null = null

function render() {
  if (!chart) return
  const palette = getChartTheme()
  chart.setOption({
    grid: { left: 8, right: 16, top: 24, bottom: 8, containLabel: true },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      backgroundColor: palette.tooltipBackground,
      borderColor: palette.border,
      shadowColor: palette.tooltipShadow,
      textStyle: { color: palette.tooltipText, fontSize: 12 },
    },
    xAxis: { type: 'value', axisLabel: { color: palette.axis }, splitLine: { lineStyle: { color: palette.grid, type: 'dashed' } } },
    yAxis: { type: 'category', data: props.data.map((d) => d.name), axisLabel: { color: palette.label } },
    series: [
      {
        type: 'bar',
        data: props.data.map((d) => d.value),
        barMaxWidth: 18,
        itemStyle: { color: palette.primary, borderRadius: [0, 4, 4, 0] },
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
watch(() => props.data, render, { deep: true })
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  window.removeEventListener(THEME_CHANGE_EVENT, render)
  chart?.dispose()
  chart = null
})
</script>

<style scoped>
.chart {
  height: 280px;
  width: 100%;
}
</style>
