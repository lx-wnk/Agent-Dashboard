import { ref } from 'vue'

export interface PresetEntry {
  tool: string
  pattern: string | null
}

export interface PresetProjectSummary {
  projectCwd: string
  entries: PresetEntry[]
}

const presets = ref<PresetProjectSummary[]>([])

async function load(): Promise<void> {
  try {
    const res = await fetch('/api/settings/permission-presets')
    if (!res.ok)
      return
    presets.value = await res.json() as PresetProjectSummary[]
  }
  catch {
    // silently ignore — strip hides itself when presets is empty
  }
}

async function revoke(cwd: string): Promise<void> {
  const res = await fetch('/api/settings/permission-presets', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ cwd }),
  })
  if (!res.ok)
    throw new Error(`Failed to revoke preset for ${cwd}`)
  await load()
}

export function usePermissionPresets() {
  return { presets, load, revoke }
}
