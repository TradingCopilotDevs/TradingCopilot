import { onBeforeUnmount, onMounted, ref, unref, type Ref } from 'vue'

type MaybeRef<T> = T | Ref<T>

interface AutoRefreshOptions {
  enabled?: MaybeRef<boolean>
  immediate?: boolean
  intervalMs: MaybeRef<number>
  refresh: () => Promise<void> | void
  refreshOnVisible?: boolean
}

export function useAutoRefresh(options: AutoRefreshOptions) {
  const isRefreshing = ref(false)
  const lastError = ref<unknown>(null)
  const lastRefreshedAt = ref<Date | null>(null)
  let timer: ReturnType<typeof window.setTimeout> | undefined
  let disposed = false
  let inFlight = false

  function isEnabled() {
    return options.enabled === undefined || Boolean(unref(options.enabled))
  }

  function isVisible() {
    return typeof document === 'undefined' || document.visibilityState === 'visible'
  }

  function clearTimer() {
    if (timer !== undefined) {
      window.clearTimeout(timer)
      timer = undefined
    }
  }

  function schedule() {
    clearTimer()
    if (disposed || !isEnabled() || !isVisible()) return
    const interval = Number(unref(options.intervalMs))
    if (!Number.isFinite(interval) || interval <= 0) return
    timer = window.setTimeout(tick, interval)
  }

  async function runRefresh() {
    if (disposed || !isEnabled() || !isVisible() || inFlight) return
    inFlight = true
    isRefreshing.value = true
    try {
      await options.refresh()
      lastError.value = null
      lastRefreshedAt.value = new Date()
    } catch (error) {
      lastError.value = error
    } finally {
      inFlight = false
      isRefreshing.value = false
    }
  }

  async function tick() {
    await runRefresh()
    schedule()
  }

  async function refreshNow() {
    await runRefresh()
    schedule()
  }

  function handleVisibilityChange() {
    if (isVisible() && (options.refreshOnVisible ?? true)) {
      void refreshNow()
      return
    }
    schedule()
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', handleVisibilityChange)
    if (options.immediate) {
      void refreshNow()
      return
    }
    schedule()
  })

  onBeforeUnmount(() => {
    disposed = true
    clearTimer()
    document.removeEventListener('visibilitychange', handleVisibilityChange)
  })

  return { isRefreshing, lastError, lastRefreshedAt, refreshNow }
}
