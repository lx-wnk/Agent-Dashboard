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

const MAX_429_RETRIES = 3
const DEFAULT_RETRY_AFTER_MS = 1000
const MAX_RETRY_AFTER_MS = 5000

function retryDelayMs(res: Response): number {
  const seconds = Number.parseInt(res.headers.get('Retry-After') ?? '', 10)
  const ms = Number.isFinite(seconds) && seconds >= 0 ? seconds * 1000 : DEFAULT_RETRY_AFTER_MS
  return Math.min(ms, MAX_RETRY_AFTER_MS)
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms))
}

// The dashboard's own boot burst can exhaust the shared per-IP rate limiter
// (server/internal/api/middleware.go); a 429 here is transient load, not a
// real failure, so both the load and the save get a few retries before
// reporting failure.
async function fetchWithRateLimitRetry(input: string, init?: RequestInit): Promise<Response> {
  let res = await fetch(input, init)
  for (let attempt = 0; attempt < MAX_429_RETRIES && res.status === 429; attempt++) {
    await sleep(retryDelayMs(res))
    res = await fetch(input, init)
  }
  return res
}

async function load(): Promise<void> {
  loading ??= (async () => {
    try {
      const res = await fetchWithRateLimitRetry('/api/settings')
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
      locked.value = 'The saved layout could not be loaded, so the built-in one is shown. Editing is locked; reload the page to try again.'
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
      const res = await fetchWithRateLimitRetry(`/api/settings/${SETTING}`, {
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

// Resolves once the write is queued, not once it lands; saveError reports the outcome.
async function save(next: WorkspaceLayout): Promise<boolean> {
  if (locked.value || !loaded.value)
    return false
  void write(next)
  return true
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
