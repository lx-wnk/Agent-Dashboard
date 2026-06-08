import type { SlotAddon, SlotAddonModule } from '../utils/pluginSlot'

interface PluginInfo {
  id: string
  capabilities: string[]
}

interface LoadDeps {
  fetchPlugins?: () => Promise<PluginInfo[]>
  importAddon?: (url: string) => Promise<SlotAddonModule>
}

async function defaultFetchPlugins(): Promise<PluginInfo[]> {
  const res = await fetch('/api/settings/plugins', { credentials: 'same-origin' })
  if (!res.ok)
    return []
  return res.json()
}

// `@vite-ignore` keeps Vite from trying to resolve the plugin URL at build time —
// it is served at runtime by the plugin process via the dashboard reverse proxy.
function defaultImportAddon(url: string): Promise<SlotAddonModule> {
  return import(/* @vite-ignore */ url)
}

/**
 * Discover route_extension plugins that provide a FE addon for `slot`.
 * Security: only plugins enumerated by `/api/settings/plugins` (registry-discovered,
 * health-checked) are imported — never an arbitrary URL.
 */
export async function loadSlotAddons(slot: string, deps: LoadDeps = {}): Promise<SlotAddon[]> {
  const fetchPlugins = deps.fetchPlugins ?? defaultFetchPlugins
  const importAddon = deps.importAddon ?? defaultImportAddon

  const plugins = await fetchPlugins()
  const candidates = plugins.filter(p => p.capabilities.includes('route_extension'))

  const addons: SlotAddon[] = []
  for (const p of candidates) {
    try {
      const mod = await importAddon(`/api/settings/plugins/${p.id}/addon.js`)
      if (mod.default?.slot === slot)
        addons.push(mod.default)
    }
    catch {
      // No addon.js (404) or bad module — skip this plugin, others continue.
    }
  }
  return addons
}
