import { onMounted, ref } from 'vue'

export interface HeatmapData {
  grid: number[][]
}

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
    error.value = e instanceof Error ? e.message : 'Failed to load heatmap'
  }
  finally {
    loading.value = false
  }
}

export function useCostHeatmap() {
  onMounted(fetchHeatmap)

  return {
    grid,
    loading,
    error,
    refetch: fetchHeatmap,
  }
}
