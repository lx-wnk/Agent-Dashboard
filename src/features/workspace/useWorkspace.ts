import type { WorkspaceLayout, WorkspacePage } from './layout'
import { ref } from 'vue'
import { DEFAULT_LAYOUT, parseLayout, serializeLayout } from './layout'

const SETTING = 'workspace.layout'

const layout = ref<WorkspaceLayout>(DEFAULT_LAYOUT)
const loaded = ref(false)
const locked = ref<string | null>(null)
const saveError = ref<string | null>(null)
const editing = ref(false)
let loading: Promise<void> | null = null
// Saves run one after another, so an older layout can never land after a newer one.
let saving: Promise<void> = Promise.resolve()

async function load(): Promise<void> {
  loading ??= (async () => {
    try {
      const res = await fetch('/api/settings')
      if (!res.ok)
        throw new Error(`HTTP ${res.status}`)
      const items = await res.json() as Array<{ key: string, value: string }>
      const parsed = parseLayout(items.find(i => i.key === SETTING)?.value ?? '')
      layout.value = parsed.layout
      locked.value = parsed.unreadable
        ? 'The saved layout could not be read, so the built-in one is shown. Editing is locked until you reset it.'
        : null
    }
    catch {
      locked.value = 'The saved layout could not be loaded, so the built-in one is shown. Editing is locked until it loads.'
    }
    finally {
      loaded.value = true
    }
  })()
  return loading
}

async function write(next: WorkspaceLayout): Promise<void> {
  layout.value = next
  saveError.value = null
  saving = saving.then(async () => {
    try {
      const res = await fetch(`/api/settings/${SETTING}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ value: serializeLayout(next) }),
      })
      if (!res.ok)
        throw new Error(`HTTP ${res.status}`)
    }
    catch (e) {
      saveError.value = `Not saved (${e instanceof Error ? e.message : 'network error'}). The change stays on screen; the next edit tries again.`
    }
  })
  return saving
}

async function save(next: WorkspaceLayout): Promise<void> {
  if (locked.value)
    return
  return write(next)
}

async function reset(): Promise<void> {
  locked.value = null
  return write(DEFAULT_LAYOUT)
}

function page(id: string): WorkspacePage | undefined {
  return layout.value.pages.find(p => p.id === id)
}

export function useWorkspace() {
  return { layout, loaded, locked, saveError, editing, load, save, reset, page }
}
