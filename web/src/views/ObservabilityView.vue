<template>
  <a-card :bordered="true">
    <template #title>{{ t('page.observability') }}</template>
    <template #extra>
      <a-space :size="8">
        <a-radio-group v-model:value="scope" size="small" button-style="solid" @change="load">
          <a-radio-button value="tool">{{ t('observability.scopeTool') }}</a-radio-button>
          <a-radio-button value="server">{{ t('observability.scopeServer') }}</a-radio-button>
        </a-radio-group>
        <a-button size="small" @click="load">
          <template #icon><ReloadOutlined /></template>{{ t('observability.refresh') }}
        </a-button>
      </a-space>
    </template>
    <a-table :data-source="rows" :columns="columns" :loading="loading" :pagination="false" :row-key="(r: any) => r.key">
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'success_rate'">{{ (record.success_rate * 100).toFixed(1) }}%</template>
      </template>
    </a-table>
  </a-card>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { getMetrics, getServerMetrics } from '@/api'
import type { MetricSnapshot } from '@/types'

const { t } = useI18n()
const scope = ref<'tool' | 'server'>('tool')
const metrics = ref<MetricSnapshot[]>([])
const loading = ref(false)

// 按当前维度展示：Tool 显示工具维；Server 去掉 server: 前缀显示 Server id。
const rows = computed<MetricSnapshot[]>(() =>
  metrics.value.map((m) =>
    scope.value === 'server' && m.key.startsWith('server:')
      ? { ...m, key: m.key.slice('server:'.length) }
      : m,
  ),
)

const columns = computed<any[]>(() => [
  { title: t('observability.dimension'), key: 'key', dataIndex: 'key' },
  { title: t('observability.requests'), key: 'totals', dataIndex: 'totals', width: 110 },
  { title: t('observability.success'), key: 'success', dataIndex: 'success', width: 100 },
  { title: t('observability.errors'), key: 'errors', dataIndex: 'errors', width: 100 },
  { title: t('observability.successRate'), key: 'success_rate', width: 120 },
  { title: t('observability.p50'), key: 'p50', dataIndex: 'p50', width: 110 },
  { title: t('observability.p95'), key: 'p95', dataIndex: 'p95', width: 110 },
  { title: t('observability.p99'), key: 'p99', dataIndex: 'p99', width: 110 },
])

onMounted(load)

async function load() {
  loading.value = true
  try {
    metrics.value = scope.value === 'server' ? await getServerMetrics() : await getMetrics()
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}
</script>
