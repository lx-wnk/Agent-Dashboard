import type { AdapterMeta, SpawnerAdapterType } from '../types'
import { ref, shallowRef } from 'vue'

// Module-level cache: the catalog is a static server constant (see
// server/internal/pipeline/adapter_catalog.go) — one fetch per page load
// is enough.
const catalog = shallowRef<AdapterMeta[]>([])
const isLoading = ref(false)
const error = ref<string | null>(null)
let inflight: Promise<void> | null = null

async function loadCatalog(): Promise<void> {
  if (catalog.value.length > 0)
    return
  if (inflight) {
    await inflight
    return
  }
  isLoading.value = true
  error.value = null
  inflight = (async () => {
    try {
      const res = await fetch('/api/adapters')
      if (!res.ok)
        throw new Error(`HTTP ${res.status}`)
      catalog.value = await res.json() as AdapterMeta[]
    }
    catch (err) {
      error.value = (err as Error).message
    }
    finally {
      isLoading.value = false
      inflight = null
    }
  })()
  await inflight
}

export function useAdapterCatalog() {
  if (catalog.value.length === 0 && !inflight)
    void loadCatalog()

  function getByType(type: SpawnerAdapterType): AdapterMeta | undefined {
    return catalog.value.find(a => a.name === type)
  }

  return {
    catalog,
    isLoading,
    error,
    reload: loadCatalog,
    getByType,
  }
}
