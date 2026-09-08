<template>
  <div class="schema-editor">
    <div class="editor-head">
      <!-- 含嵌套/复合结构时表格表达不了，自动退回 JSON 并说明原因 -->
      <a-alert
        v-if="mode === 'json' && reason === 'nested'"
        type="warning"
        show-icon
        class="head-alert"
        :message="t('tools.schemaNestedOnly')"
      />
      <a-radio-group :value="mode" button-style="solid" size="small" @change="onModeChange">
        <a-radio-button value="table">{{ t('toolDetail.viewTable') }}</a-radio-button>
        <a-radio-button value="json">JSON</a-radio-button>
      </a-radio-group>
    </div>

    <template v-if="mode === 'table'">
      <div class="row head">
        <span class="c-name muted">{{ t('tools.paramName') }}</span>
        <span class="c-type muted">{{ t('tools.paramType') }}</span>
        <span class="c-req muted" :title="t('tools.paramRequired')">{{ t('tools.paramRequired') }}</span>
        <span class="c-enum muted" :title="t('tools.enumHint')">{{ t('tools.enumCol') }}</span>
        <span class="c-def muted">{{ t('tools.paramDefault') }}</span>
        <span class="c-desc muted">{{ t('tools.paramDesc') }}</span>
        <span class="c-op" />
      </div>
      <div v-for="row in rows" :key="row.id" class="row">
        <a-input v-model:value="row.name" class="c-name mono" size="small" @change="sync" />
        <a-select v-model:value="row.type" class="c-type" size="small" :options="typeOptions" @change="onTypeChange(row)" />
        <a-checkbox v-model:checked="row.required" class="c-req" @change="sync" />
        <div class="c-enum">
          <a-popover trigger="click" placement="bottomLeft" :overlay-inner-style="{ width: '300px' }">
            <template #content>
              <div class="enum-ed">
                <div class="enum-head">
                  <span class="muted">{{ t('tools.enumValues') }}</span>
                  <a-button
                    v-if="row.enumVals.length"
                    type="link"
                    size="small"
                    class="enum-clear"
                    @click="clearEnum(row)"
                  >{{ t('tools.clearEnum') }}</a-button>
                </div>
                <div v-if="row.enumVals.length" class="enum-tags">
                  <a-tag v-for="(v, i) in row.enumVals" :key="i" closable class="enum-tag" @close="removeEnum(row, i)">{{
                    enumTextOf(v)
                  }}</a-tag>
                </div>
                <div class="enum-add">
                  <a-input
                    v-model:value="row.enumText"
                    size="small"
                    :placeholder="t('tools.enumAddPlaceholder')"
                    @press-enter="addEnum(row)"
                  />
                  <a-button size="small" type="primary" ghost @click="addEnum(row)">{{ t('tools.enumAdd') }}</a-button>
                </div>
                <div v-if="!row.enumVals.length" class="muted enum-empty">{{ t('tools.enumEmpty') }}</div>
              </div>
            </template>
            <a-button size="small" class="enum-trigger" :class="{ has: row.enumVals.length }">
              {{ row.enumVals.length ? `${t('tools.enumLabel')} (${row.enumVals.length})` : t('tools.enumLabel') }}
            </a-button>
          </a-popover>
        </div>
        <div class="c-def">
          <a-input
            v-if="row.type === 'string'"
            v-model:value="row.textDef"
            size="small"
            placeholder="—"
            @change="markDefault(row); sync()"
          />
          <a-input-number
            v-else-if="row.type === 'number' || row.type === 'integer'"
            v-model:value="row.numDef"
            class="num"
            size="small"
            :controls="false"
            placeholder="—"
            @change="markDefault(row); sync()"
          />
          <a-select
            v-else
            v-model:value="row.boolDef"
            class="bool"
            size="small"
            :options="boolOptions"
            @change="markDefault(row); sync()"
          />
        </div>
        <a-input v-model:value="row.description" class="c-desc" size="small" @change="sync" />
        <a-button type="text" size="small" class="c-op del" @click="removeRow(row)">
          <template #icon><DeleteOutlined /></template>
        </a-button>
      </div>
      <a-button type="dashed" block class="add" @click="addParam">
        <template #icon><PlusOutlined /></template>{{ t('tools.addParam') }}
      </a-button>
      <div v-if="tableError" class="err">{{ tableError }}</div>
    </template>

    <a-textarea v-else v-model:value="text" :rows="12" class="mono json" spellcheck="false" />
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue'

const props = defineProps<{ modelValue: string; seed?: number }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const { t } = useI18n()

// 表格只能表达一层标量参数；其余结构走 JSON，避免表格重建时丢嵌套字段。
const SCALAR_TYPES = ['string', 'number', 'integer', 'boolean']
const typeOptions = SCALAR_TYPES.map((v) => ({ value: v, label: v }))
const boolOptions = [
  { value: 'none', label: t('tools.noDefault') },
  { value: 'true', label: 'true' },
  { value: 'false', label: 'false' },
]

type RowType = 'string' | 'number' | 'integer' | 'boolean'

interface Row {
  id: number
  name: string
  type: RowType
  required: boolean
  description: string
  // 默认值编辑缓冲：按类型分开存放，空白表示“无默认值”。
  textDef: string
  numDef: number | null
  boolDef: string
  // 用户主动改过默认值才覆盖原值；未触碰时原样保留（含无法用控件表达的默认值）。
  touchedDef: boolean
  origHasDef: boolean
  origDef: unknown
  // 受限取值（枚举）：上游原始值类型可能不统一，逐项新增时按 row.type 收敛。
  enumVals: unknown[]
  enumText: string
  // 属性中表格不负责的其余键，重建时透传，避免丢 format/minimum 等。
  extra: Record<string, unknown>
}

const mode = ref<'table' | 'json'>('table')
const reason = ref<'nested' | ''>('')
const text = ref(props.modelValue ?? '')
const rows = ref<Row[]>([])
const carry = ref<Record<string, unknown>>({})
const tableError = ref('')

let uid = 0
let lastSeed = props.seed ?? 0
let inited = false

function emitText(v: string) {
  text.value = v
  emit('update:modelValue', v)
}

function parseObj(s: string): Record<string, any> | null {
  if (!s) return null
  try {
    const o = JSON.parse(s)
    return o && typeof o === 'object' && !Array.isArray(o) ? (o as Record<string, any>) : null
  } catch {
    return null
  }
}

// 抽取属性的受限取值：enum / const，以及 anyOf·oneOf 分支里的 const/enum。
// 与详情只读表/手动表单（@/utils/schema）同一套口径，保证“上游给了枚举”的地方都能预填。
function extractEnum(prop: Record<string, any>): unknown[] {
  const out: unknown[] = []
  const push = (v: unknown) => {
    if (!out.some((x) => x === v)) out.push(v)
  }
  if (Array.isArray(prop.enum)) prop.enum.forEach(push)
  else if ('const' in prop) push(prop.const)
  for (const combo of ['anyOf', 'oneOf']) {
    const branches = prop[combo]
    if (!Array.isArray(branches)) continue
    for (const b of branches) {
      if (!b || typeof b !== 'object' || Array.isArray(b)) continue
      const br = b as Record<string, any>
      if (Array.isArray(br.enum)) br.enum.forEach(push)
      else if ('const' in br) push(br.const)
    }
  }
  return out
}

// 一组枚举值能否归为单一标量列类型（编辑行只能承载一种类型）。
function inferEnumType(vals: unknown[]): RowType | null {
  if (!vals.length) return null
  if (vals.every((v) => typeof v === 'boolean')) return 'boolean'
  if (vals.every((v) => typeof v === 'number')) {
    return vals.every((v) => Number.isInteger(v as number)) ? 'integer' : 'number'
  }
  if (vals.every((v) => typeof v === 'string')) return 'string'
  return null
}

// 判断属性可承载为一行标量编辑：显式标量 type，或带可推断类型的受限取值。
// anyOf/oneOf 只在其能收敛成常量枚举时按标量处理，否则仍是复合结构（走 JSON）。
function scalarRowTypeOf(p: Record<string, any>): RowType | null {
  const hasCombo =
    (Array.isArray(p.anyOf) && p.anyOf.length > 0) || (Array.isArray(p.oneOf) && p.oneOf.length > 0)
  if (hasCombo) return inferEnumType(extractEnum(p))
  if (typeof p.type === 'string' && SCALAR_TYPES.includes(p.type)) return p.type as RowType
  return inferEnumType(extractEnum(p))
}

// 某属性是否可安全落入一行表格：必须是标量类型（含枚举推断），无子结构/复合结构。
function isScalarProp(p: unknown): boolean {
  if (!p || typeof p !== 'object' || Array.isArray(p)) return false
  return scalarRowTypeOf(p as Record<string, any>) !== null
}

// 顶层是否可安全用表格表达：properties 均为标量；required 都落在 properties 内。
function representable(schema: Record<string, any>): boolean {
  if (schema.type !== undefined && schema.type !== 'object') return false
  const props = schema.properties
  if (props !== undefined && (!props || typeof props !== 'object' || Array.isArray(props))) return false
  const required = schema.required
  if (required !== undefined && !Array.isArray(required)) return false
  if (Array.isArray(required) && props) {
    for (const n of required) if (!(n in props)) return false
  }
  for (const p of Object.values(props ?? {})) {
    if (!isScalarProp(p)) return false
  }
  return true
}

function buildRows(schema: Record<string, any>) {
  const props = schema.properties ?? {}
  const required = new Set(Array.isArray(schema.required) ? schema.required : [])
  const c = { ...schema }
  delete c.properties
  delete c.required
  carry.value = c
  rows.value = Object.entries(props).map(([name, p]) => {
    const prop = (p ?? {}) as Record<string, any>
    const extra = { ...prop }
    delete extra.type
    delete extra.description
    delete extra.default
    delete extra.enum // 枚举单独由 enumVals 列编辑，不再混在透传键里
    const origHasDef = 'default' in prop
    const origDef = origHasDef ? prop.default : undefined
    // 与展示口径一致地预填受限取值；无 type 的 const/分支枚举也能推断成标量行。
    const enumVals = extractEnum(prop)
    const type = scalarRowTypeOf(prop) ?? 'string'
    // 常量/分支写法收敛成 “type + enum” 行：不再透传原 const/anyOf/oneOf，避免重建冲突。
    if (enumVals.length) {
      delete extra.const
      delete extra.anyOf
      delete extra.oneOf
    }
    let textDef = ''
    let numDef: number | null = null
    let boolDef = 'none'
    if (origHasDef) {
      const d = origDef
      if (type === 'string' && typeof d === 'string') textDef = d
      else if ((type === 'number' || type === 'integer') && typeof d === 'number') numDef = d
      else if (type === 'boolean' && typeof d === 'boolean') boolDef = d ? 'true' : 'false'
    }
    return {
      id: ++uid,
      name,
      type,
      required: required.has(name),
      description: typeof prop.description === 'string' ? prop.description : '',
      textDef,
      numDef,
      boolDef,
      touchedDef: false,
      origHasDef,
      origDef,
      enumVals,
      enumText: '',
      extra,
    }
  })
  tableError.value = ''
}

// 校验行的名称合法且不重复；不通过时写内联错误并返回 false。
function validateRows(): boolean {
  const names = new Set<string>()
  for (const r of rows.value) {
    const n = r.name.trim()
    if (!n || names.has(n)) {
      tableError.value = t('tools.paramNameConflict')
      return false
    }
    names.add(n)
  }
  tableError.value = ''
  return true
}

function buildProp(row: Row): Record<string, unknown> {
  const prop: Record<string, unknown> = { ...row.extra, type: row.type }
  const desc = row.description.trim()
  if (desc) prop.description = desc
  else delete prop.description
  if (row.enumVals.length) prop.enum = row.enumVals
  else delete prop.enum
  if (row.touchedDef) {
    if (row.type === 'string') {
      const v = row.textDef
      if (v) prop.default = v
      else delete prop.default
    } else if (row.type === 'number' || row.type === 'integer') {
      if (row.numDef !== null && row.numDef !== undefined) {
        prop.default = row.type === 'integer' ? Math.trunc(row.numDef) : row.numDef
      } else delete prop.default
    } else {
      if (row.boolDef === 'true') prop.default = true
      else if (row.boolDef === 'false') prop.default = false
      else delete prop.default
    }
  } else if (row.origHasDef) {
    prop.default = row.origDef
  } else {
    delete prop.default
  }
  return prop
}

function buildFromRows(): Record<string, unknown> {
  const out: Record<string, unknown> = { ...carry.value }
  if (rows.value.length) {
    const props: Record<string, unknown> = {}
    for (const r of rows.value) props[r.name] = buildProp(r)
    out.properties = props
    const req = rows.value.filter((r) => r.required).map((r) => r.name)
    if (req.length) out.required = req
    else delete out.required
  }
  return out
}

// 行状态 → JSON 文本（仅当行合法）；行非法只提示不改文本。
function sync(): boolean {
  if (!validateRows()) return false
  emitText(JSON.stringify(buildFromRows(), null, 2))
  return true
}

// 保存/切模式前的统一提交：返回规范化 JSON 文本，非法返回 null。
function commit(): string | null {
  if (mode.value === 'table') {
    if (!sync()) {
      message.warning(t('tools.paramNameConflict'))
      return null
    }
    return text.value
  }
  const obj = parseObj(text.value)
  if (!obj) {
    message.warning(t('tools.invalidSchema'))
    return null
  }
  const norm = JSON.stringify(obj, null, 2)
  emitText(norm)
  return norm
}

// 表格模式下已改过默认值则标记，重建时以控件值为准。
function markDefault(row: Row) {
  row.touchedDef = true
}

function onTypeChange(row: Row) {
  // 类型切换后清空默认值缓冲，但未触碰时仍保留原默认值。
  row.textDef = ''
  row.numDef = null
  row.boolDef = 'none'
  // 原枚举可能不再适配新类型，随类型一并清空由用户重新录入。
  row.enumVals = []
  row.enumText = ''
  sync()
}

function addParam() {
  const names = new Set(rows.value.map((r) => r.name))
  let n = rows.value.length + 1
  while (names.has(`param${n}`)) n += 1
  rows.value.push({
    id: ++uid,
    name: `param${n}`,
    type: 'string',
    required: false,
    description: '',
    textDef: '',
    numDef: null,
    boolDef: 'none',
    touchedDef: false,
    origHasDef: false,
    origDef: undefined,
    enumVals: [],
    enumText: '',
    extra: {},
  })
  sync()
}

function removeRow(row: Row) {
  rows.value = rows.value.filter((r) => r.id !== row.id)
  sync()
}

// —— 枚举（受限取值）编辑 ——

function enumTextOf(v: unknown): string {
  if (typeof v === 'string') return v
  if (v === null) return 'null'
  return typeof v === 'number' || typeof v === 'boolean' ? String(v) : JSON.stringify(v)
}

// 把输入文本按当前列类型收敛成合法取值；不合法或为空返回 ok:false。
function parseEnumToken(row: Row): { ok: boolean; val?: unknown } {
  const s = (row.enumText ?? '').trim()
  if (!s) return { ok: false }
  if (row.type === 'string') return { ok: true, val: s }
  if (row.type === 'boolean') {
    if (s === 'true') return { ok: true, val: true }
    if (s === 'false') return { ok: true, val: false }
    return { ok: false }
  }
  const n = Number(s)
  if (!Number.isFinite(n)) return { ok: false }
  if (row.type === 'integer' && !Number.isInteger(n)) return { ok: false }
  return { ok: true, val: row.type === 'integer' ? Math.trunc(n) : n }
}

// 枚举取值与类型不符或重复时的内联提示。
function enumInvalid(row: Row) {
  message.warning(`${row.name}: ${t('tools.enumValueInvalid')}`)
}

function addEnum(row: Row) {
  const r = parseEnumToken(row)
  if (!r.ok || r.val === undefined) {
    enumInvalid(row)
    return
  }
  if (row.enumVals.some((x) => x === r.val || JSON.stringify(x) === JSON.stringify(r.val))) {
    enumInvalid(row)
    return
  }
  row.enumVals.push(r.val)
  row.enumText = ''
  sync()
}

function removeEnum(row: Row, index: number) {
  row.enumVals.splice(index, 1)
  sync()
}

function clearEnum(row: Row) {
  row.enumVals = []
  row.enumText = ''
  sync()
}

function onModeChange(e: { target: { value: string } }) {
  const target = e.target.value as 'table' | 'json'
  if (target === mode.value) return
  if (target === 'table') {
    const obj = parseObj(text.value)
    if (!obj) {
      message.warning(t('tools.invalidSchema'))
      return
    }
    if (!representable(obj)) {
      reason.value = 'nested'
      message.warning(t('tools.schemaNestedOnly'))
      return
    }
    reason.value = ''
    buildRows(obj)
    mode.value = 'table'
  } else {
    if (!sync()) return // 行非法（空/重名）则停在表格并提示
    reason.value = ''
    mode.value = 'json'
  }
}

function init(v: string) {
  text.value = v
  reason.value = ''
  tableError.value = ''
  const obj = parseObj(v)
  if (!obj || !representable(obj)) {
    mode.value = 'json'
    if (obj) reason.value = 'nested'
    return
  }
  buildRows(obj)
  mode.value = 'table'
}

// 来源变化（打开另一工具 / 恢复上游 / 由外层整体替换）时按最新 modelValue 重建；
// 自身 emit 导致的 prop 回写值相同，不会触发。
watch(
  () => props.modelValue,
  (v) => {
    // 首次进入或外层整体替换（值已变）时重建；自身 emit 导致的回写值相同则跳过。
    if (!inited || v !== text.value) {
      inited = true
      init(v)
    }
  },
  { immediate: true },
)
watch(
  () => props.seed,
  (s) => {
    if (s !== undefined && s !== lastSeed) {
      lastSeed = s
      inited = true
      init(props.modelValue)
    }
  },
)

defineExpose({ commit })
</script>

<style scoped>
.editor-head {
  display: flex;
  align-items: flex-start;
  justify-content: flex-end;
  gap: 12px;
  margin-bottom: 8px;
}
.head-alert {
  flex: 1;
  margin: 0;
}
.row {
  display: grid;
  grid-template-columns: 150px 120px 34px 92px 128px minmax(0, 1fr) 28px;
  column-gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}
.row.head {
  margin-bottom: 4px;
  font-size: 12px;
}
.c-req {
  justify-self: center;
}
.c-type,
.bool {
  width: 100%;
}
.c-enum {
  min-width: 0;
}
.enum-trigger.has {
  color: var(--mc-accent-light);
  border-color: rgba(59, 130, 246, 0.35);
}
.num {
  width: 100%;
}
.del {
  padding: 0;
  color: var(--mc-ink-3);
}
.add {
  margin-top: 4px;
}
.enum-ed {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.enum-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.enum-clear {
  padding: 0;
  height: auto;
}
.enum-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  max-height: 96px;
  overflow: auto;
}
.enum-tag {
  margin-inline-end: 0;
}
.enum-add {
  display: flex;
  gap: 6px;
}
.enum-add .ant-input-affix-wrapper,
.enum-add .ant-input {
  flex: 1;
  min-width: 0;
}
.enum-empty {
  font-size: 12px;
}
.err {
  color: var(--mc-danger-light);
  font-size: 12px;
  margin-top: 6px;
}
.json {
  font-family: var(--mc-mono);
}
.muted {
  color: var(--mc-ink-3);
}
</style>
