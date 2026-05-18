import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const width = ref(typeof window === 'undefined' ? 1440 : window.innerWidth)
let initialized = false
let listeners = 0

function handleResize() {
  width.value = window.innerWidth
}

function bindResize() {
  if (!initialized && typeof window !== 'undefined') {
    window.addEventListener('resize', handleResize, { passive: true })
    initialized = true
  }
  listeners += 1
}

function unbindResize() {
  listeners = Math.max(0, listeners - 1)
  if (initialized && listeners === 0 && typeof window !== 'undefined') {
    window.removeEventListener('resize', handleResize)
    initialized = false
  }
}

export function useResponsive() {
  onMounted(bindResize)
  onBeforeUnmount(unbindResize)

  const isMobile = computed(() => width.value <= 767)
  const isTablet = computed(() => width.value >= 768 && width.value <= 1023)
  const isDesktop = computed(() => width.value >= 1024)
  const isMobileNav = computed(() => width.value < 1024)

  return {
    width,
    isMobile,
    isTablet,
    isDesktop,
    isMobileNav
  }
}
