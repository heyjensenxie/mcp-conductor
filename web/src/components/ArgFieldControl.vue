<template>
  <!-- 枚举 → 下拉；boolean → 开关；number/integer → 数字；其余字符串输入 -->
  <a-select
    v-if="enumOptions.length"
    :value="enumModelValue"
    :options="enumOptions"
    :allow-clear="allowClear"
    :placeholder="placeholder"
    size="small"
    class="ctrl"
    @change="(v: unknown) => emit('update:modelValue', v === undefined ? null : v)"
  />
  <a-switch
    v-else-if="scalar === 'boolean'"
    :checked="Boolean(modelValue)"
    size="small"
    class="ctrl"
    @change="(v: boolean) => emit('update:modelValue', v)"
  />
  <a-input-number
    v-else-if="scalar === 'number' || scalar === 'integer'"
    :value="modelValue == null ? null : Number(modelValue)"
    :allow-clear="allowClear"
    :placeholder="placeholder"
    class="ctrl"
    controls
    @change="(v: number | null) => emit('update:modelValue', v)"
  />
  <a-input
    v-else
    :value="modelValue == null ? '' : String(modelValue)"
    :allow-clear="allowClear"
    :placeholder="placeholder"
    class="ctrl"
    @change="(p: unknown) => emit('update:modelValue', readTextInput(p))"
  />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { EnumVal, ScalarKind } from '@/utils/schema'

const props = withDefaults(
  defineProps<{
    scalar: ScalarKind
    enumVals?: EnumVal[] | null
    modelValue?: unknown
    allowClear?: boolean
    placeholder?: string
  }>(),
  { enumVals: null, modelValue: undefined, allowClear: false, placeholder: '' },
)
const emit = defineEmits<{ (e: 'update:modelValue', value: unknown): void }>()

const { t } = useI18n()

// a-input/a-textarea 的 @change 首参是原生事件（如 CompositionEvent），不是值；
// 从 e.target.value 取值，并兼容“直接给值字符串”的第三方实现。
function readTextInput(p: unknown): string {
  if (p && typeof p === 'object' && 'target' in p) {
    const tv = (p as { target?: { value?: unknown } }).target?.value
    return typeof tv === 'string' ? tv : ''
  }
  return typeof p === 'string' ? p : ''
}

const enumOptions = computed(() =>
  (props.enumVals ?? []).map((v) => ({ value: v.value as string | number | boolean, label: v.text })),
)
const placeholder = computed(() => props.placeholder || t('toolDetail.selectHint'))

// 枚举下拉的空态：'' 会顶掉 antd 的占位文案（仅 null/undefined 才显示）。
// 若 '' 本身是合法枚举值（罕见）则原样展示；否则视为“未选”，以露出占位提示。
const enumModelValue = computed(() =>
  props.modelValue === '' && !enumOptions.value.some((o) => o.value === '') ? undefined : props.modelValue,
)
</script>

<style scoped>
.ctrl {
  width: 100%;
}
</style>
