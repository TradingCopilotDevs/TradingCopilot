import { computed, effectScope, ref, watch } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'
export type ThemePalette = 'emerald' | 'blue' | 'violet' | 'coral' | 'graphite'

const MODE_STORAGE_KEY = 'tradingcopilot_theme_mode'
const PALETTE_STORAGE_KEY = 'tradingcopilot_theme_palette'
const themeMode = ref<ThemeMode>(readStoredThemeMode())
const themePalette = ref<ThemePalette>(readStoredThemePalette())
const systemPrefersDark = ref(false)
const resolvedTheme = computed<ResolvedTheme>(() => {
  if (themeMode.value === 'system') return systemPrefersDark.value ? 'dark' : 'light'
  return themeMode.value
})

let initialized = false
const themeEffectScope = effectScope(true)

function isThemeMode(value: unknown): value is ThemeMode {
  return value === 'light' || value === 'dark' || value === 'system'
}

function isThemePalette(value: unknown): value is ThemePalette {
  return value === 'emerald' || value === 'blue' || value === 'violet' || value === 'coral' || value === 'graphite'
}

function readStoredThemeMode(): ThemeMode {
  if (typeof window === 'undefined') return 'system'
  try {
    const stored = window.localStorage.getItem(MODE_STORAGE_KEY)
    return isThemeMode(stored) ? stored : 'system'
  } catch {
    return 'system'
  }
}

function readStoredThemePalette(): ThemePalette {
  if (typeof window === 'undefined') return 'emerald'
  try {
    const stored = window.localStorage.getItem(PALETTE_STORAGE_KEY)
    return isThemePalette(stored) ? stored : 'emerald'
  } catch {
    return 'emerald'
  }
}

function persistThemeMode(value: ThemeMode) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(MODE_STORAGE_KEY, value)
  } catch {
    // Storage can be unavailable in privacy-restricted contexts.
  }
}

function persistThemePalette(value: ThemePalette) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(PALETTE_STORAGE_KEY, value)
  } catch {
    // Storage can be unavailable in privacy-restricted contexts.
  }
}

function applyTheme() {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.dataset.theme = resolvedTheme.value
  root.dataset.themeMode = themeMode.value
  root.dataset.palette = themePalette.value
  root.style.colorScheme = resolvedTheme.value
}

function bindSystemTheme() {
  if (typeof window === 'undefined' || !window.matchMedia) return
  const query = window.matchMedia('(prefers-color-scheme: dark)')
  systemPrefersDark.value = query.matches

  const handleChange = (event: MediaQueryListEvent) => {
    systemPrefersDark.value = event.matches
  }

  if (query.addEventListener) {
    query.addEventListener('change', handleChange)
  } else {
    query.addListener(handleChange)
  }
}

function initializeTheme() {
  if (initialized) return
  initialized = true
  bindSystemTheme()
  themeEffectScope.run(() => {
    watch(themeMode, persistThemeMode, { immediate: true })
    watch(themePalette, persistThemePalette, { immediate: true })
    watch([themeMode, resolvedTheme, themePalette], applyTheme, { immediate: true })
  })
}

export function useTheme() {
  initializeTheme()

  return {
    themeMode,
    themePalette,
    resolvedTheme,
    setThemeMode: (mode: ThemeMode) => {
      themeMode.value = mode
    },
    setThemePalette: (palette: ThemePalette) => {
      themePalette.value = palette
    }
  }
}
