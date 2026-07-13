import { onMounted, ref } from 'vue'
import { errorMessage } from '@/utils/errorMessage'

export interface NotifPref {
  eventType: string
  channels: string[]
  enabled: boolean
}

export function useNotificationConfig() {
  const prefs = ref<Record<string, NotifPref>>({})
  const config = ref<Record<string, string>>({})
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function fetchNotificationConfig() {
    loading.value = true
    error.value = null
    try {
      const [prefRes, cfgRes] = await Promise.all([
        fetch('/api/notifications/preferences'),
        fetch('/api/notifications/config'),
      ])
      if (!prefRes.ok || !cfgRes.ok)
        throw new Error(`Failed to load notification settings (HTTP ${prefRes.ok ? cfgRes.status : prefRes.status})`)

      const prefList: NotifPref[] = await prefRes.json()
      prefs.value = prefList.reduce<Record<string, NotifPref>>((acc, p) => {
        acc[p.eventType] = p
        return acc
      }, {})

      config.value = await cfgRes.json()
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to load notifications')
    }
    finally {
      loading.value = false
    }
  }

  async function savePref(eventType: string, updated: NotifPref): Promise<NotifPref> {
    const res = await fetch(`/api/notifications/preferences/${eventType}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ channels: updated.channels, enabled: updated.enabled }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const saved: NotifPref = await res.json()
    prefs.value = { ...prefs.value, [eventType]: saved }
    return saved
  }

  async function saveConfig(configValue: Record<string, string>): Promise<Record<string, string>> {
    const res = await fetch('/api/notifications/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(configValue),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const saved: Record<string, string> = await res.json()
    config.value = saved
    return saved
  }

  onMounted(fetchNotificationConfig)

  return {
    prefs,
    config,
    loading,
    error,
    refetch: fetchNotificationConfig,
    savePref,
    saveConfig,
  }
}
