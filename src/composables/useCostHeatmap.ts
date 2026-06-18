import { onMounted, ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

export interface HeatmapData {
  grid: number[][]
}

export function useCostHeatmap() {
  const grid = ref<number[][]>(Array.from({ length: 7 }, () => Array.from<number>({ length: 24 }).fill(0)))
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchHeatmap() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/analytics/heatmap')
      if (!res.ok)
        throw new Error(await res.text())
      const data = await res.json() as HeatmapData
      grid.value = data.grid
    }
    catch (e: unknown) {
      error.value = errorMessage(e, 'Failed to load heatmap')
    }
    finally {
      loading.value = false
    }
  }

  onMounted(fetchHeatmap)

  return {
    grid,
    loading,
    error,
    refetch: fetchHeatmap,
  }
}
