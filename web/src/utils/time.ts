// 后端统一时间解析。后端所有时间字段输出 UTC "YYYY-MM-DD HH:mm:ss"
// （空格分隔、无时区后缀、秒级精度），该格式不是严格 ISO，JS 的
// new Date(str) 各引擎解析行为不一致（部分会按本地时区解释），
// 必须显式按 UTC 构造 Date。兼容旧 RFC3339 / ISO 字符串（如本地
// localStorage 元数据用 toISOString 写入的值）兜底交给原生解析。

const BACKEND_TIME_RE = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})$/

/** 把后端时间串解析为 Date（空格分隔格式视为 UTC；其余走原生解析兜底）。 */
export function parseBackendTime(s: string): Date {
  const m = BACKEND_TIME_RE.exec(s)
  if (m) {
    return new Date(Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]))
  }
  return new Date(s)
}

/** 解析后端时间串为毫秒时间戳（供图表/比较用）；失效时返回 NaN。 */
export function backendTimeMs(s: string): number {
  return parseBackendTime(s).getTime()
}