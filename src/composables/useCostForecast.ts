import { onMounted, ref } from 'vue'

export interface ForecastTrendPoint { t: number, y: number }
export interface ForecastPoint { t: number, projectedCost: number }
export interface ForecastAlert { level: 'warn' | 'critical', message: string }

export interface ForecastData {
  trend: ForecastTrendPoint[]
  forecast: ForecastPoint[]
  alerts: ForecastAlert[]
}

export function useCostForecast() {
  const trend = ref<ForecastTrendPoint[]>([])
  const forecast = ref<ForecastPoint[]>([])
  const alerts = ref<ForecastAlert[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchForecast() {
    loading.value = true
    error.value = null
    try {
      const res = await fetch('/api/analytics/cost-forecast')
      if (!res.ok)
        throw new Error(await res.text())
      const data = await res.json() as ForecastData
      trend.value = data.trend
      forecast.value = data.forecast
      alerts.value = data.alerts
    }
    catch (e: unknown) {
      error.value = e instanceof Error ? e.message : 'Failed to load forecast'
    }
    finally {
      loading.value = false
    }
  }

  onMounted(fetchForecast)

  return {
    trend,
    forecast,
    alerts,
    loading,
    error,
    refetch: fetchForecast,
  }
}
