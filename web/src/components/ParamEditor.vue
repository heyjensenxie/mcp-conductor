<template>
  <div v-for="node in fields" :key="node.path" class="pe-field">
    <div class="pe-row">
      <div class="pe-label">
        <span class="pe-key mono">{{ node.key }}</span>
        <span v-if="node.required" class="req">*</span>
        <div v-if="node.desc" class="pe-desc">{{ node.desc }}</div>
      </div>

      <div class="pe-control">
        <template v-if="kindOf(node) === 'scalar'">
          <ArgFieldControl
            :scalar="node.scalar as ScalarKind"
            :enum-vals="node.enumVals"
            :model-value="model[node.key]"
            :allow-clear="!node.required"
            @update:model-value="model[node.key] = $event"
          />
        </template>

        <template v-else-if="kindOf(node) === 'object'">
          <div class="pe-nested">
            <ParamEditor :fields="node.props ?? []" :model="model[node.key]" />
          </div>
        </template>

        <template v-else-if="kindOf(node) === 'arrMulti'">
          <a-select
            mode="multiple"
            :value="asArr(node)"
            :options="(node.itemEnum ?? []).map((v) => ({ label: v.text, value: v.value as string | number }))"
            :allow-clear="!node.required"
            class="w100"
            size="small"
            :placeholder="t('toolDetail.selectHint')"
            @change="(vals: (string | number)[]) => (model[node.key] = vals)"
          />
        </template>

        <template v-else-if="kindOf(node) === 'arrRows'">
          <div class="pe-rows">
            <div v-for="(row, i) in asArr(node)" :key="i" class="pe-rowline">
              <ArgFieldControl
                :scalar="node.itemScalar as ScalarKind"
                :enum-vals="node.itemEnum"
                :model-value="row.v"
                @update:model-value="row.v = $event"
              />
              <a-button type="text" danger size="small" class="ic" :title="t('toolDetail.removeItem')" @click="removeRow(node, i)">
                <template #icon><DeleteOutlined /></template>
              </a-button>
            </div>
            <a-button type="dashed" size="small" class="pe-add" @click="addRow(node)">
              <template #icon><PlusOutlined /></template>{{ t('toolDetail.addItem') }}
            </a-button>
          </div>
        </template>

        <template v-else-if="kindOf(node) === 'arrObj'">
          <div class="pe-objs">
            <div v-for="(element, i) in asArr(node)" :key="i" class="pe-obj">
              <div class="pe-obj-head">
                <span class="pe-obj-tag mono">[{{ i }}]</span>
                <a-button type="text" danger size="small" class="ic" :title="t('toolDetail.removeItem')" @click="removeRow(node, i)">
                  <template #icon><DeleteOutlined /></template>
                </a-button>
              </div>
              <ParamEditor :fields="node.elementProps ?? []" :model="element" />
            </div>
            <a-button type="dashed" size="small" class="pe-add" @click="addRow(node)">
              <template #icon><PlusOutlined /></template>{{ t('toolDetail.addItem') }}
            </a-button>
          </div>
        </template>

        <a-textarea
          v-else
          class="w100 mono"
          :value="asText(node)"
          :rows="2"
          :placeholder="`${node.typeText} — ${t('toolDetail.jsonArgHint')}`"
          spellcheck="false"
          @change="(p: unknown) => (model[node.key] = textFrom(p))"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue'
import ArgFieldControl from './ArgFieldControl.vue'
import { editorKindFor, objectSkeleton } from '@/utils/schema'
import type { ParamNode, ScalarKind } from '@/utils/schema'

// 允许组件在自身模板中递归渲染 object / array<object> 子结构。
defineOptions({ name: 'ParamEditor' })

const props = defineProps<{ fields: ParamNode[]; model: Record<string, any> }>()

const { t } = useI18n()

const kindOf = (n: ParamNode) => editorKindFor(n)

// a-textarea 的 @change 首参是原生事件（如 CompositionEvent），不是值；取 target.value。
function textFrom(p: unknown): string {
  if (p && typeof p === 'object' && 'target' in p) {
    const tv = (p as { target?: { value?: unknown } }).target?.value
    return typeof tv === 'string' ? tv : ''
  }
  return typeof p === 'string' ? p : ''
}

// 数组控件读写统一入口：model[node.key] 已由骨架保证是数组；防御性兜底。
function asArr(n: ParamNode): any[] {
  const v = props.model[n.key]
  if (!Array.isArray(v)) {
    props.model[n.key] = []
    return props.model[n.key]
  }
  return v
}

function asText(n: ParamNode): string {
  const v = props.model[n.key]
  return v == null ? '' : String(v)
}

// 标量行空值起点：boolean 从 false 开始，其余留空待填。
function rowInit(scalar: ScalarKind | null): unknown {
  if (scalar === 'boolean') return false
  return undefined
}

function addRow(n: ParamNode) {
  const arr = asArr(n)
  if (editorKindFor(n) === 'arrObj') {
    arr.push(objectSkeleton(n.elementProps ?? []))
  } else {
    arr.push({ v: rowInit(n.itemScalar) })
  }
}

function removeRow(n: ParamNode, index: number) {
  asArr(n).splice(index, 1)
}
</script>

<style scoped>
.pe-field {
  width: 100%;
}
.pe-row {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 4px 0;
}
.pe-label {
  flex: 0 0 200px;
  min-width: 200px;
}
.pe-key {
  word-break: break-all;
}
.req {
  color: #ff4d4f;
  margin-left: 2px;
}
.pe-desc {
  font-size: 12px;
  color: var(--mc-ink-3);
  margin-top: 2px;
  word-break: break-word;
}
.pe-control {
  flex: 1;
  min-width: 0;
}
.pe-nested,
.pe-rows,
.pe-objs {
  padding: 8px 10px;
  border: 1px solid var(--mc-border, #f0f0f0);
  border-radius: 6px;
  background: var(--mc-bg-soft, #fafafa);
}
.pe-rows,
.pe-objs {
  padding: 8px;
}
.pe-rowline {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-bottom: 6px;
}
.pe-rowline .w100 {
  flex: 1;
}
.pe-obj-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}
.pe-obj-tag {
  color: var(--mc-ink-3);
  font-size: 12px;
}
.ic {
  flex: none;
  padding: 0 6px;
}
.pe-add {
  margin-top: 2px;
}
.w100 {
  width: 100%;
}
</style>
