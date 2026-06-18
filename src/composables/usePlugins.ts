import type { PluginInfo } from '../utils/plugins'
import { onMounted, ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'
import { fetchPluginList } from '../utils/plugins'

export type { PluginInfo }

export function usePlugins() {
  const plugins = ref<PluginInfo[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchPlugins() {
    loading.value = true
    error.value = null
    try {
      plugins.value = await fetchPluginList()
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to load plugins')
    }
    finally {
      loading.value = false
    }
  }

  onMounted(fetchPlugins)

  return {
    plugins,
    loading,
    error,
    refetch: fetchPlugins,
  }
}
