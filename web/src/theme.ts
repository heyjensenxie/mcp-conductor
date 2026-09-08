export type ThemeMode = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'

export const THEME_STORAGE_KEY = 'mcp-conductor-theme'
export const THEME_CHANGE_EVENT = 'mc-theme-change'

const systemThemeQuery = typeof window === 'undefined' ? null : window.matchMedia('(prefers-color-scheme: dark)')
let initialized = false

function isThemeMode(value: string | null): value is ThemeMode {
  return value === 'light' || value === 'dark' || value === 'system'
}

export function getThemeMode(): ThemeMode {
  if (typeof window === 'undefined') return 'system'
  const stored = window.localStorage.getItem(THEME_STORAGE_KEY)
  return isThemeMode(stored) ? stored : 'system'
}

export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode !== 'system') return mode
  return systemThemeQuery?.matches ? 'dark' : 'light'
}

export function applyTheme(mode: ThemeMode): ResolvedTheme {
  const resolved = resolveTheme(mode)
  document.documentElement.dataset.theme = resolved
  document.documentElement.dataset.themeMode = mode
  return resolved
}

function notifyThemeChange(mode: ThemeMode): void {
  window.dispatchEvent(new CustomEvent(THEME_CHANGE_EVENT, { detail: { mode, resolved: resolveTheme(mode) } }))
}

export function setThemeMode(mode: ThemeMode): ResolvedTheme {
  window.localStorage.setItem(THEME_STORAGE_KEY, mode)
  const resolved = applyTheme(mode)
  notifyThemeChange(mode)
  return resolved
}

export function initializeTheme(): void {
  if (initialized || typeof window === 'undefined') return
  initialized = true
  applyTheme(getThemeMode())
  systemThemeQuery?.addEventListener('change', () => {
    const mode = getThemeMode()
    if (mode !== 'system') return
    applyTheme(mode)
    notifyThemeChange(mode)
  })
}

// Execute before the Vue app mounts so the first painted surface already has a theme.
initializeTheme()

export const mcpDarkChartTheme = {
  primary: '#3b82f6',
  secondary: '#22d3ee',
  purple: '#8b5cf6',
  success: '#22c55e',
  danger: '#ef4444',
  axis: '#64748b',
  label: '#94a3b8',
  grid: 'rgba(148,163,184,0.08)',
  border: 'rgba(148,163,184,0.12)',
  tooltipBackground: '#0f2032',
  tooltipText: '#f1f5f9',
  tooltipShadow: 'rgba(0,0,0,0.3)',
  areaStart: 'rgba(59,130,246,0.14)',
}

export const mcpLightChartTheme = {
  primary: '#2563eb',
  secondary: '#06b6d4',
  purple: '#7c3aed',
  success: '#16a34a',
  danger: '#dc2626',
  axis: '#94a3b8',
  label: '#64748b',
  grid: 'rgba(15,23,42,0.07)',
  border: '#e2e8f0',
  tooltipBackground: 'rgba(255,255,255,0.96)',
  tooltipText: '#0f172a',
  tooltipShadow: 'rgba(15,23,42,0.12)',
  areaStart: 'rgba(37,99,235,0.14)',
}

export type ChartTheme = typeof mcpDarkChartTheme

export function getChartTheme(): ChartTheme {
  return resolveTheme(getThemeMode()) === 'dark' ? mcpDarkChartTheme : mcpLightChartTheme
}
