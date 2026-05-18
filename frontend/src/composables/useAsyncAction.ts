import { ElMessage } from 'element-plus'
import { reactive } from 'vue'
import { apiErrorText } from '../api'

interface AsyncActionOptions {
  success?: string
  error?: string
}

export function useAsyncAction() {
  const running = reactive<Record<string, boolean>>({})

  async function runAction<T>(key: string, action: () => Promise<T>, options: AsyncActionOptions = {}) {
    if (running[key]) return undefined
    running[key] = true
    try {
      const result = await action()
      if (options.success) ElMessage.success(options.success)
      return result
    } catch (error) {
      ElMessage.error(apiErrorText(error, options.error))
      return undefined
    } finally {
      running[key] = false
    }
  }

  return { running, runAction }
}
