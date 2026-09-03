<template>
  <div ref="el" class="chart"></div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts'

export interface ChartDatum {
  name: string
  value: number
}

const props = defineProps<{ data: ChartDatum[] }>()

const el = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null

function render() {
  if (!chart) return
  chart.setOption({
    grid: { left: 8, right: 16, top: 24, bottom: 8, containLabel: true },
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    xAxis: { type: 'value' },
    yAxis: { type: 'category', data: props.data.map((d) => d.name) },
    series: [
      {
        type: 'bar',
        data: props.data.map((d) => d.value),
        barMaxWidth: 18,
        itemStyle: { color: '#1677ff', borderRadius: [0, 4, 4, 0] },
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
watch(() => props.data, render, { deep: true })
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
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