<template>
  <a-card :bordered="true">
    <template #title>{{ title }}</template>
    <template #extra>
      <!-- 默认表格视图，需要原始定义时手动切到 JSON -->
      <a-radio-group v-model:value="mode" button-style="solid" size="small">
        <a-radio-button value="table">{{ t('toolDetail.viewTable') }}</a-radio-button>
        <a-radio-button value="json">JSON</a-radio-button>
      </a-radio-group>
    </template>

    <template v-if="mode === 'table'">
      <a-table
        v-if="rows.length"
        size="small"
        :columns="columns"
        :data-source="rows"
        :pagination="false"
        row-key="key"
        :scroll="{ x: 'max-content', y: 400 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <span v-if="record.kind === 'more'" class="more-row muted">…</span>
            <span v-else class="mono cell-path" :title="record.node.path">{{ record.node.path }}</span>
          </template>

          <template v-else-if="column.key === 'type'">
            <template v-if="record.kind === 'more'"><span class="muted">…</span></template>
            <template v-else>
              <a-tag :bordered="false" :color="typeColorFor(record.node.colorKey)" class="type-tag">{{
                record.node.typeText
              }}</a-tag>
              <a-tooltip v-if="record.node.more" :title="t('toolDetail.deeperCut')">
                <span class="muted cut-dot">▾…</span>
              </a-tooltip>
            </template>
          </template>

          <template v-else-if="column.key === 'required'">
            <template v-if="record.kind === 'more'"><span class="muted">—</span></template>
            <span v-else :class="record.node.required ? 'req' : 'muted'">
              {{ record.node.required ? t('common.yes') : t('common.no') }}
            </span>
          </template>

          <template v-else-if="column.key === 'allowed'">
            <template v-if="record.kind === 'more'"><span class="muted">—</span></template>
            <template v-else>
              <template v-if="record.enumTags.length">
                <div class="enum">
                  <a-tag v-for="ev in record.enumTags" :key="ev" :bordered="false" class="enum-tag">{{ ev }}</a-tag>
                  <span v-if="record.enumMore" class="muted enum-more">+{{ record.enumMore }}</span>
                </div>
              </template>
              <span v-else class="muted">—</span>
            </template>
          </template>

          <template v-else-if="column.key === 'default'">
            <template v-if="record.kind === 'more'"><span class="muted">—</span></template>
            <template v-else>
              <span v-if="record.node.hasDefault" class="mono cell-def">{{ record.node.defaultText || '""' }}</span>
              <span v-else class="muted">—</span>
            </template>
          </template>

          <template v-else-if="column.key === 'description'">
            <template v-if="record.kind === 'more'"><span class="muted">—</span></template>
            <template v-else>
              <div v-if="record.node.desc" class="desc">{{ record.node.desc }}</div>
              <span v-else class="muted">—</span>
            </template>
          </template>
        </template>
      </a-table>
      <div v-else class="no-props muted">{{ t('toolDetail.schemaNoProps') }}</div>
    </template>

    <pre v-else class="mono json-view">{{ pretty }}</pre>
  </a-card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { schemaToNodes } from '@/utils/schema'
import type { ParamNode } from '@/utils/schema'

const props = defineProps<{ title: string; schema?: Record<string, any> | null }>()

const { t } = useI18n()
const mode = ref<'table' | 'json'>('table')

// JSON-Schema 顶层 properties 中常见类型的着色；复合类型按基础类型归色。
const TYPE_COLORS: Record<string, string> = {
  string: 'green',
  integer: 'geekblue',
  number: 'geekblue',
  boolean: 'purple',
  array: 'cyan',
  object: 'orange',
  default: 'default',
}
function typeColorFor(colorKey: string): string {
  return TYPE_COLORS[colorKey] ?? 'default'
}

// 受限取值只展示前 12 个，其余折叠为 +N，避免长枚举把表格撑得很高。
const ENUM_SHOW_LIMIT = 12

interface Row {
  key: number
  kind: 'node' | 'more'
  node?: ParamNode
  enumTags: string[]
  enumMore: number
}

const schema = computed<Record<string, any>>(() => (props.schema && typeof props.schema === 'object' ? props.schema : {}))

// 节点树扁平化：object 子属性 / array 元素 object 的子属性，递归展开为带缩进的行。
function flatten(nodes: ParamNode[], out: Row[]): void {
  for (const n of nodes) {
    const ownEnum = n.enumVals ?? (n.isArrayScalar ? n.itemEnum : null)
    out.push({
      key: out.length,
      kind: 'node',
      node: n,
      enumTags: ownEnum ? ownEnum.slice(0, ENUM_SHOW_LIMIT).map((v) => v.text) : [],
      enumMore: ownEnum ? Math.max(ownEnum.length - ENUM_SHOW_LIMIT, 0) : 0,
    })
    if (n.props && n.props.length) flatten(n.props, out)
    else if (n.isArrayObject && n.elementProps && n.elementProps.length) flatten(n.elementProps, out)
    else if (n.more) out.push({ key: out.length, kind: 'more', enumTags: [], enumMore: 0 })
  }
}

const rows = computed<Row[]>(() => {
  const out: Row[] = []
  flatten(schemaToNodes(schema.value), out)
  return out
})

const columns = computed<any[]>(() => [
  { title: t('toolDetail.param'), key: 'name', width: 230 },
  { title: t('toolDetail.typeCol'), key: 'type', width: 150 },
  { title: t('toolDetail.requiredCol'), key: 'required', width: 72 },
  { title: t('toolDetail.allowedCol'), key: 'allowed', width: 220 },
  { title: t('toolDetail.defaultCol'), key: 'default', width: 150 },
  { title: t('common.description'), key: 'description' },
])

const pretty = computed(() => JSON.stringify(schema.value, null, 2))
</script>

<style scoped>
.type-tag {
  font-family: var(--mc-mono);
}
.cell-path {
  word-break: break-all;
}
.cell-def {
  word-break: break-all;
}
.desc {
  word-break: break-word;
  white-space: pre-wrap;
}
.enum {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px;
}
.enum-tag {
  margin-inline-end: 0;
}
.enum-more {
  font-size: 12px;
  align-self: center;
}
.cut-dot {
  margin-left: 4px;
}
.req {
  color: #ff4d4f;
}
.muted {
  color: var(--mc-ink-3);
}
.more-row {
  font-size: 13px;
  padding-left: 2px;
}
.no-props {
  padding: 20px 8px;
  text-align: center;
}
.json-view {
  margin: 0;
  max-height: 400px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
