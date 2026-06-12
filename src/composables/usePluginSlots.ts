import type { PluginInfo } from '../utils/plugins'
import type { LoadedAddon, SlotAddonModule, SlotName } from '../utils/pluginSlot'
import { fetchPluginList } from '../utils/plugins'
import { PLUGIN_UI_CAPABILITY } from '../utils/pluginSlot'

/** Per-plugin UI manifest served at `/api/settings/plugins/{id}/ui-manifest.json`. */
export interface UiManifest {
  slots: { slot: string, module: string }[]
}

interface LoadDeps {
  fetchPlugins?: () => Promise<PluginInfo[]>
  fetchManifest?: (pluginId: string) => Promise<UiManifest | null>
  importAddon?: (url: string) => Promise<SlotAddonModule>
}

// Module-level memo caches. Production calls loadSlotAddons() with no deps and shares
// these across every slot lookup, so the plugin list + each manifest + each module is
// fetched/imported at most once per page load. Tests inject deps and reset between cases.
let pluginListCache: Promise<PluginInfo[]> | null = null
const manifestCache = new Map<string, Promise<UiManifest | null>>()
const moduleCache = new Map<string, Promise<SlotAddonModule>>()

/** Clear all memo caches. Test-only: keeps cases deterministic. */
export function resetSlotCaches(): void {
  pluginListCache = null
  manifestCache.clear()
  moduleCache.clear()
}

// `@vite-ignore` keeps Vite from resolving the plugin URL at build time — it is served
// at runtime by the plugin process via the dashboard reverse proxy.
function defaultImportAddon(url: string): Promise<SlotAddonModule> {
  return import(/* @vite-ignore */ url)
}

async function defaultFetchManifest(pluginId: string): Promise<UiManifest | null> {
  const res = await fetch(`/api/settings/plugins/${pluginId}/ui-manifest.json`, { credentials: 'same-origin' })
  if (!res.ok)
    return null
  return res.json()
}

function getPlugins(fetchPlugins: () => Promise<PluginInfo[]>): Promise<PluginInfo[]> {
  if (!pluginListCache) {
    pluginListCache = (async () => {
      try {
        return await fetchPlugins()
      }
      catch (err) {
        pluginListCache = null // allow a later retry after a transient failure
        throw err
      }
    })()
  }
  return pluginListCache
}

function getManifest(
  pluginId: string,
  fetchManifest: (id: string) => Promise<UiManifest | null>,
): Promise<UiManifest | null> {
  let cached = manifestCache.get(pluginId)
  if (!cached) {
    // Cache null too: a missing/404 manifest is a stable answer for this page load.
    cached = fetchManifest(pluginId).catch(() => null)
    manifestCache.set(pluginId, cached)
  }
  return cached
}

function importModule(url: string, importAddon: (url: string) => Promise<SlotAddonModule>): Promise<SlotAddonModule> {
  let cached = moduleCache.get(url)
  if (!cached) {
    cached = importAddon(url)
    moduleCache.set(url, cached)
  }
  return cached
}

/**
 * Discover plugin addons that target `slot`.
 *
 * Security: only plugins enumerated by `/api/settings/plugins` (registry-discovered,
 * health-checked) are considered, and modules are imported only from that plugin's
 * SSRF-guarded proxy path — never an arbitrary URL.
 *
 * Resolution per candidate plugin (has `ui_extension` or `route_extension`):
 *  1. UI manifest present → import only the module(s) the manifest maps to `slot`.
 *  2. No manifest (legacy) → import `addon.js` and match on `mod.default.slot`.
 */
export async function loadSlotAddons(slot: SlotName, deps: LoadDeps = {}): Promise<LoadedAddon[]> {
  const fetchPlugins = deps.fetchPlugins ?? fetchPluginList
  const fetchManifest = deps.fetchManifest ?? defaultFetchManifest
  const importAddon = deps.importAddon ?? defaultImportAddon

  let plugins: PluginInfo[]
  try {
    plugins = await getPlugins(fetchPlugins)
  }
  catch {
    return [] // plugin list unavailable → slot degrades to empty
  }

  const candidates = plugins.filter(p =>
    p.capabilities.includes(PLUGIN_UI_CAPABILITY) || p.capabilities.includes('route_extension'))

  const addons: LoadedAddon[] = []
  for (const p of candidates) {
    try {
      const manifest = await getManifest(p.id, fetchManifest)
      if (manifest && Array.isArray(manifest.slots)) {
        for (const entry of manifest.slots) {
          if (entry.slot !== slot)
            continue
          const mod = await importModule(`/api/settings/plugins/${p.id}/${entry.module}`, importAddon)
          if (mod.default)
            addons.push({ ...mod.default, slot })
        }
        continue
      }
      // Legacy fallback: addon.js declares its own slot.
      const mod = await importModule(`/api/settings/plugins/${p.id}/addon.js`, importAddon)
      if (mod.default?.slot === slot)
        addons.push(mod.default)
    }
    catch {
      // Missing module (404) or bad import — skip this plugin, others continue.
    }
  }
  return addons
}
