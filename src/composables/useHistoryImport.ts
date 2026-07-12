import { onUnmounted, ref } from 'vue'

export interface HistoryImportProgress {
  total: number
  processed: number
  imported?: number
  errors?: number
  done: boolean
}

export interface UseHistoryImportOptions {
  /** Invoked once the server reports the scan finished (`done: true`). */
  onDone?: (progress: HistoryImportProgress) => void
}

/**
 * Owns the POST /api/history/import + EventSource('/api/history/import/status')
 * lifecycle shared by ApiKeySettings and CostAnalyticsView: kicks off the scan,
 * tolerates a 409 ("already running") by attaching to the live stream anyway,
 * and treats a malformed SSE frame like a stream error instead of throwing.
 */
export function useHistoryImport(options: UseHistoryImportOptions = {}) {
  const isImporting = ref(false)
  const importStatus = ref('')
  let importEs: EventSource | null = null

  function stop() {
    importEs?.close()
    importEs = null
    isImporting.value = false
  }

  function attachStream() {
    importEs = new EventSource('/api/history/import/status')
    importEs.onmessage = (ev) => {
      let progress: HistoryImportProgress
      try {
        progress = JSON.parse(ev.data)
      }
      catch {
        importStatus.value = 'Connection lost — import may still be running'
        stop()
        return
      }
      importStatus.value = `Scanning… ${progress.processed}/${progress.total}`
      if (progress.done) {
        importStatus.value = `Imported ${progress.imported ?? progress.processed} sessions`
        stop()
        options.onDone?.(progress)
      }
    }
    importEs.onerror = () => {
      importStatus.value = 'Connection lost — import may still be running'
      stop()
    }
  }

  async function start() {
    if (isImporting.value)
      return
    isImporting.value = true
    importStatus.value = 'Starting…'
    try {
      const res = await fetch('/api/history/import', { method: 'POST' })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        const errMsg = (body as { error?: string }).error ?? res.statusText
        if (res.status === 409) {
          // Already running — still attach to the stream for live progress.
          importStatus.value = `${errMsg} — watching progress…`
        }
        else {
          importStatus.value = `Error: ${errMsg}`
          isImporting.value = false
          return
        }
      }
      else {
        importStatus.value = 'Scanning…'
      }
      attachStream()
    }
    catch (e) {
      importStatus.value = `Error: ${e instanceof Error ? e.message : String(e)}`
      isImporting.value = false
    }
  }

  onUnmounted(stop)

  return { isImporting, importStatus, start, stop }
}
