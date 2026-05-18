import { ref } from 'vue'

export interface CursorPage<T> {
  items: T[]
  nextCursor: string
}

export function nextCursorFromDocument(document: unknown) {
  return String((document as any)?.meta?.nextCursor || '')
}

export function useCursorPagination<T>(
  fetchPage: (cursor?: string) => Promise<CursorPage<T>>
) {
  const items = ref<T[]>([])
  const nextCursor = ref('')
  const loading = ref(false)
  const loadingMore = ref(false)

  async function loadFirstPage() {
    loading.value = true
    try {
      const page = await fetchPage()
      items.value = page.items as any
      nextCursor.value = page.nextCursor
    } finally {
      loading.value = false
    }
  }

  async function refreshFirstPage() {
    const page = await fetchPage()
    items.value = page.items as any
    nextCursor.value = page.nextCursor
  }

  async function loadMore() {
    if (!nextCursor.value || loadingMore.value) return
    loadingMore.value = true
    try {
      const page = await fetchPage(nextCursor.value)
      items.value = items.value.concat(page.items as any) as any
      nextCursor.value = page.nextCursor
    } finally {
      loadingMore.value = false
    }
  }

  function reset() {
    items.value = []
    nextCursor.value = ''
  }

  return {
    items,
    nextCursor,
    loading,
    loadingMore,
    loadFirstPage,
    refreshFirstPage,
    loadMore,
    reset
  }
}
