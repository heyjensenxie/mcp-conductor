// 入参 JSON-Schema 的结构化解析（纯只读，无 Vue 依赖）。
//
// 供两处共用，保证表格与表单对“枚举 / 数组 / 嵌套对象”的理解一致：
//   1. ToolDetailView 顶部只读参数表（InputSchemaView）
//   2. ToolDetailView 底部“手动调用”按 Schema 生成的参数表单（ParamEditor）
//
// 只关心 MCP tools/input 常见的 JSON-Schema 子集：
//   - type：标量，或数组联合（如 ["string","null"]）
//   - 受限取值：enum / const，以及 anyOf / oneOf 里以 const/enum 表达的分支
//   - object.properties（含自身的 required）
//   - array.items（标量 / 含枚举 / 元素为 object / 极少见的 tuple）
//
// 不展开 $ref / 远程引用；所有递归都受 MAX_SCHEMA_DEPTH 限制，遇到更深结构用
// `more` 标记截断，杜绝环或异常深的嵌套把渲染拖垮。

export const MAX_SCHEMA_DEPTH = 5

export type ScalarKind = 'string' | 'number' | 'integer' | 'boolean'
const SCALAR_KINDS: ScalarKind[] = ['string', 'number', 'integer', 'boolean']

export interface EnumVal {
  text: string
  value: unknown
}

// 解析后的单个入参属性（树节点）。children 两类都叫 props：
//  object 属性   -> node.isObject && node.props（自身子属性）
//  array<object> -> node.isArrayObject && node.elementProps（每个元素的子属性）
export interface ParamNode {
  key: string
  path: string
  required: boolean
  desc: string
  depth: number
  // —— 编辑友好类型 ——
  scalar: ScalarKind | null // string/number/integer/boolean（含 enum 标量）
  enumVals: EnumVal[] | null // 属性自身的受限取值（如 type=string + enum）
  // —— 结构类型 ——
  isObject: boolean
  props: ParamNode[] | null // 仅 isObject 时：可编辑的子属性，null = 更深被截断/无结构
  isArrayScalar: boolean // array，元素是标量/枚举
  itemScalar: ScalarKind | null
  itemEnum: EnumVal[] | null // 数组元素的受限取值（如 items.enum）
  isArrayObject: boolean // array，元素是 object
  elementProps: ParamNode[] | null
  isTuple: boolean // array，items 是 tuple（编辑器退回 JSON）
  // —— 展示辅助 ——
  typeText: string
  colorKey: string
  hasDefault: boolean
  defaultText: string
  more: boolean // 深度达上限，更深结构已折叠
}

function asObj(v: unknown): Record<string, any> | null {
  if (v !== null && typeof v === 'object' && !Array.isArray(v)) return v as Record<string, any>
  return null
}

// 枚举 / 值集的受限取值转展示文本（与默认值同一套序列化）。
export function valueText(v: unknown): string {
  if (typeof v === 'string') return v
  if (v === null) return 'null'
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  return JSON.stringify(v)
}

function inferScalarFromValue(v: unknown): ScalarKind | null {
  if (typeof v === 'string') return 'string'
  if (typeof v === 'boolean') return 'boolean'
  if (typeof v === 'number') return Number.isInteger(v) ? 'integer' : 'number'
  return null
}

// 收集一个 schema 的原始受限取值：enum / const / anyOf·oneOf 分支里的 const/enum。
function collectEnum(prop: Record<string, any>): unknown[] | null {
  const out: unknown[] = []
  const pushList = (v: unknown) => {
    if (Array.isArray(v)) v.forEach((x) => out.push(x))
  }
  if (Array.isArray(prop.enum)) pushList(prop.enum)
  else if ('const' in prop) out.push(prop.const)
  for (const combo of ['anyOf', 'oneOf']) {
    const branches = prop[combo]
    if (!Array.isArray(branches)) continue
    for (const raw of branches) {
      const b = asObj(raw)
      if (!b) continue
      if (Array.isArray(b.enum)) pushList(b.enum)
      else if ('const' in b) out.push(b.const)
    }
  }
  if (!out.length) return null
  const uniq = out.filter((x, i) => out.indexOf(x) === i)
  return uniq.length ? uniq : null
}

function toEnumVal(v: unknown): EnumVal {
  return { text: valueText(v), value: v }
}

// 归一化 type：字符串、字符串数组、或缺失（靠结构反推），含 anyOf/oneOf 的类型。
// 返回去重后的基础类型 token 列表。
function typeTokens(prop: Record<string, any>): string[] {
  const out = new Set<string>()
  const add = (t: unknown) => {
    if (typeof t === 'string' && t) out.add(t)
  }
  const tv = prop.type
  if (Array.isArray(tv)) tv.forEach(add)
  else add(tv)
  for (const combo of ['anyOf', 'oneOf', 'allOf']) {
    const arr = prop[combo]
    if (!Array.isArray(arr)) continue
    for (const raw of arr) {
      const b = asObj(raw)
      if (b) typeTokens(b).forEach((t) => out.add(t))
    }
  }
  // 有 properties 却没有 type 的旧式写法，也认作 object。
  const p = prop.properties
  if (p && typeof p === 'object' && !Array.isArray(p)) out.add('object')
  return [...out]
}

// object 的子属性 + 它自己的 required；不是对象/无 properties 时返回 null。
function objProps(prop: Record<string, any>): { entries: [string, unknown][]; required: Set<string> } | null {
  const p = prop.properties
  if (!p || typeof p !== 'object' || Array.isArray(p)) return null
  const entries = Object.entries(p)
  const req = new Set<string>(
    Array.isArray(prop.required) ? prop.required.filter((n): n is string => typeof n === 'string') : [],
  )
  return { entries, required: req }
}

// 对象嵌套入参子节点的展示用路径（含 [] 标记，如 rows[].id）。
function childPath(parentPath: string, key: string): string {
  return parentPath ? `${parentPath}.${key}` : key
}

const scalarUnionText = (kinds: string[]): string => {
  // 按 MCP 常见展示顺序排布：string number integer boolean null
  const order = ['string', 'number', 'integer', 'boolean', 'null']
  const set = new Set(kinds)
  const ordered = order.filter((k) => set.has(k))
  const rest = kinds.filter((k) => !order.includes(k))
  return [...ordered, ...rest].join(' | ')
}

function buildProp(key: string, raw: unknown, required: boolean, depth: number, path: string): ParamNode {
  const prop = asObj(raw) ?? {}
  const tokens = typeTokens(prop)
  const scalarTokens = tokens.filter((t): t is ScalarKind => (SCALAR_KINDS as string[]).includes(t))
  const hasNull = tokens.includes('null')
  const isArrayLike = tokens.includes('array') || prop.items !== undefined
  const objInfo = objProps(prop)
  const isObjectLike = tokens.includes('object') || objInfo !== null
  const nextDepth = depth + 1
  const canExpand = nextDepth <= MAX_SCHEMA_DEPTH

  const base: ParamNode = {
    key,
    path,
    required,
    desc: typeof prop.description === 'string' ? prop.description : '',
    depth,
    scalar: null,
    enumVals: null,
    isObject: false,
    props: null,
    isArrayScalar: false,
    itemScalar: null,
    itemEnum: null,
    isArrayObject: false,
    elementProps: null,
    isTuple: false,
    typeText: 'unknown',
    colorKey: 'default',
    hasDefault: 'default' in prop,
    defaultText: 'default' in prop ? valueText(prop.default) : '',
    more: false,
  }

  // —— 数组 ——
  if (isArrayLike) {
    base.isArrayScalar = false
    base.isArrayObject = false
    base.isTuple = false
    base.typeText = 'array'
    base.colorKey = 'array'

    const items = prop.items
    if (Array.isArray(items)) {
      // tuple：逐项类型不同，编辑器退回 JSON。
      base.isTuple = true
      const inner = items.map((s) => {
        const t = typeTokens(asObj(s) ?? {})
        return scalarUnionText(t.length ? t : ['unknown'])
      })
      base.typeText = inner.length ? `array<[${inner.join(', ')}]>` : 'array'
    } else {
      const item = asObj(items)
      if (item) {
        const itemTokens = typeTokens(item)
        const itemScalars = itemTokens.filter((t): t is ScalarKind => (SCALAR_KINDS as string[]).includes(t))
        const itemObj = objProps(item)
        const itemEnum = collectEnum(item)
        // 元素为可编辑标量
        if (itemScalars.length === 1 && !itemObj && !itemTokens.includes('array')) {
          const s = itemScalars[0]
          base.isArrayScalar = true
          base.itemScalar = s
          base.itemEnum = itemEnum ? itemEnum.map(toEnumVal) : null
          base.typeText = `array<${itemTokens.includes('null') ? `${s} | null` : s}>`
        } else if (itemObj && canExpand) {
          // 元素为 object：展开其子属性（每个元素共享同一份结构）。
          base.isArrayObject = true
          base.typeText = 'array<object>'
          base.elementProps = itemObj.entries.map(([ik, ip]) =>
            buildProp(ik, ip, itemObj.required.has(ik), nextDepth, `${path}[]`),
          )
        } else if (itemTokens.includes('array')) {
          base.isArrayObject = false
          const t = itemTokens.includes('null') ? 'array | null' : 'array'
          base.typeText = `array<${t}>`
        } else {
          base.typeText = 'array<unknown>'
        }
        if (base.elementProps === null && itemObj) base.more = !canExpand
      } else if (items === undefined && scalarTokens.length === 1) {
        // type 写了 array 却没 items：按元素未知处理，退回 JSON。
        base.isArrayScalar = true
        base.itemScalar = scalarTokens[0]
        base.typeText = 'array'
      }
    }
    return base
  }

  // —— object ——
  if (isObjectLike) {
    base.isObject = true
    base.colorKey = 'object'
    if (objInfo && objInfo.entries.length && canExpand) {
      base.props = objInfo.entries.map(([pk, pp]) =>
        buildProp(pk, pp, objInfo!.required.has(pk), nextDepth, childPath(path, pk)),
      )
    } else if (objInfo && objInfo.entries.length) {
      base.more = true
    }
    base.typeText = hasNull ? 'object | null' : 'object'
    return base
  }

  // —— 标量 / 受限值 / 其它 ——
  const enumRaw = collectEnum(prop)
  if (scalarTokens.length === 1 && (tokens.length === 1 || (tokens.length === 2 && hasNull))) {
    // 单一标量（可带 null）是最干净的编辑对象。
    base.scalar = scalarTokens[0]
    base.colorKey = scalarTokens[0]
    base.enumVals = enumRaw ? enumRaw.map(toEnumVal) : null
    base.typeText = scalarUnionText(tokens)
    return base
  }
  if (enumRaw) {
    // 没写 type 但给了 enum：按首值类型当标量处理。
    const first = inferScalarFromValue(enumRaw[0])
    if (first && !tokens.some((t) => t === 'object' || t === 'array')) {
      base.scalar = first
      base.colorKey = first
      base.enumVals = enumRaw.map(toEnumVal)
      base.typeText = tokens.length ? scalarUnionText(tokens) : first
      return base
    }
  }
  if ('const' in prop) {
    const c = prop.const
    const inferred = inferScalarFromValue(c)
    if (inferred) {
      base.scalar = inferred
      base.colorKey = inferred
      base.enumVals = [toEnumVal(c)]
      base.typeText = inferred
      return base
    }
  }
  // 多类型 / 结构不明的联合：保留类型展示，编辑器退回 JSON。
  base.scalar = scalarTokens.length ? scalarTokens[0] : null
  base.colorKey = scalarTokens.length ? scalarTokens[0] : 'default'
  base.enumVals = enumRaw ? enumRaw.map(toEnumVal) : null
  base.typeText = tokens.length ? scalarUnionText(tokens) : (enumRaw ? String(typeof enumRaw[0]) : 'unknown')
  return base
}

// 顶层 schema（object）的属性列表转节点树；不是标准 object 时返回空。
export function schemaToNodes(schema?: Record<string, unknown> | null): ParamNode[] {
  const s = asObj(schema)
  if (!s) return []
  const info = objProps(s)
  if (!info) return []
  return info.entries.map(([k, p]) => buildProp(k, p, info.required.has(k), 0, k))
}

// ---------------------------------------------------------------------------
// 手动调用表单使用：把节点判成对应控件形态，并给出“未填时的空容器骨架”。
// 表格与表单共用同一套判定，保证两者对结构类型的理解一致。
// ---------------------------------------------------------------------------

export type FieldEditorKind = 'scalar' | 'object' | 'arrMulti' | 'arrRows' | 'arrObj' | 'json'

// 节点 → 编辑器形态：
//   scalar   单值输入/枚举下拉（可带 enumVals）
//   object   有子属性的可编辑对象（嵌套渲染子节点）
//   arrMulti 数组元素是可枚举标量 → 多选下拉
//   arrRows  数组元素是普通标量 → 可增删的逐行输入
//   arrObj   数组元素是对象 → 可增删的对象块
//   json     其余（tuple/更深嵌套/任意结构）→ JSON 文本编辑
export function editorKindFor(n: ParamNode): FieldEditorKind {
  if (n.isArrayScalar && n.itemScalar) {
    if (n.itemEnum && n.itemEnum.length && n.itemScalar !== 'boolean') return 'arrMulti'
    return 'arrRows'
  }
  if (n.isArrayObject && n.elementProps && n.elementProps.length) return 'arrObj'
  if (n.isObject && n.props && n.props.length) return 'object'
  if (n.scalar) return 'scalar'
  return 'json'
}

// 给一组属性建“空容器骨架”：object 建空对象（递归到更深容器），array 建空数组，
// json 建空字符串；标量不占用（缺值即“未填”，收集时按必填/可选处理）。
export function objectSkeleton(fields: ParamNode[]): Record<string, any> {
  const o: Record<string, any> = {}
  for (const f of fields) {
    const kind = editorKindFor(f)
    if (kind === 'object') o[f.key] = objectSkeleton(f.props ?? [])
    else if (kind === 'arrMulti' || kind === 'arrRows' || kind === 'arrObj') o[f.key] = []
    else if (kind === 'json') o[f.key] = ''
    // 标量不初始化，保持 undefined = 未填。
  }
  return o
}
