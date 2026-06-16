// Single source for the plugin list — both usePlugins and usePluginSlots use this.
export interface PluginInfo {
  id: string
  capabilities: string[]
}

export async function fetchPluginList(): Promise<PluginInfo[]> {
  const res = await fetch('/api/settings/plugins', { credentials: 'same-origin' })
  if (!res.ok)
    throw new Error(`Failed to load plugins (HTTP ${res.status}: ${res.statusText})`)
  const data = await res.json()
  return Array.isArray(data) ? data : []
}
