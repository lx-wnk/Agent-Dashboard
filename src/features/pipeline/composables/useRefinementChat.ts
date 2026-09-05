import type { PipelineTask } from '@/types'
import { ref } from 'vue'
import { actionEndpoint } from '@/composables/useRunAction'

export interface ImageAttachment {
  dataUrl: string
  mimeType: string
}

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  phase?: string | null
  options?: string[]
  images?: ImageAttachment[]
}

export const PHASE_ORDER = ['analysis', 'spec', 'implementation_plan', 'approval'] as const
const PHASE_DONE_LINE_RE = /__phase_done:\s*(\w+)/

// Options block markers — the agent emits prepared answers between these two
// lines. Parity: the backend strips the same block in
// server/internal/refine/options.go (optionsBlockRE). Keep the two in sync.
const OPTIONS_START_LINE = '__options_start'
const OPTIONS_END_LINE = '__options_end'

export const PHASE_LABELS: Record<string, string> = {
  analysis: 'Analysis',
  spec: 'Spec',
  implementation_plan: 'Implementation Plan',
  approval: 'Approval',
}

export type RefineRunStatus = 'none' | 'refining' | 'draft_ready' | 'failed' | null

// GET /api/refine/{taskId}/turns returns a bare array of turns. Refinement
// streams inline over the POST /turn SSE response (see sendMessage), so there
// is no async job to poll for — history is a one-shot fetch.
export interface RefineTurn {
  role: string
  content: string
  phase: string | null
  options?: string[]
}

/** Single-source fetch for the refinement conversation history. Throws on non-2xx. */
export async function fetchRefineTurns(taskId: string): Promise<RefineTurn[]> {
  const res = await fetch(`/api/refine/${taskId}/turns`)
  if (!res.ok)
    throw new Error(`Failed to load refinement turns: HTTP ${res.status}`)
  return await res.json() as RefineTurn[]
}

export function lastAssistantContent(turns: RefineTurn[]): string {
  return [...turns].reverse().find(t => t.role === 'assistant')?.content ?? ''
}

export function completedPhasesFromTurns(turns: RefineTurn[]): string[] {
  return turns.flatMap(t => (t.phase ? [t.phase] : []))
}

export function useRefinementChat(taskId: () => string | null) {
  const messages = ref<ChatMessage[]>([])
  const completedPhases = ref<Set<string>>(new Set())
  const isStreaming = ref(false)
  const error = ref<string | null>(null)
  const approvalReady = ref(false)
  const runStatus = ref<RefineRunStatus>(null)

  // The detached refinement run (server-side) outlives any single POST /turn
  // request. These let us (a) abort the client read on close and (b) poll
  // GET /status to reflect a run that is still in flight when the modal reopens.
  let abortController: AbortController | null = null
  let pollTimer: ReturnType<typeof setTimeout> | null = null

  const POLL_INTERVAL_MS = 1500

  function applyTurnsToMessages(turns: RefineTurn[]) {
    // Reset phase state so switching tasks (or reloading) never carries stale
    // progress from a previous conversation.
    completedPhases.value = new Set()
    approvalReady.value = false
    messages.value = turns.map(t => ({
      role: t.role as 'user' | 'assistant',
      content: t.content,
      phase: t.phase,
      ...(t.options?.length ? { options: t.options } : {}),
    }))
    for (const t of turns) {
      if (t.phase)
        completedPhases.value.add(t.phase)
    }
    if (completedPhases.value.has('approval'))
      approvalReady.value = true
  }

  async function loadHistory() {
    const id = taskId()
    if (!id)
      return
    error.value = null
    try {
      const data = await fetchRefineTurns(id)
      applyTurnsToMessages(data)
    }
    catch {
      error.value = 'Failed to load history'
    }
  }

  async function sendMessage(message: string, images?: ImageAttachment[]) {
    const id = taskId()
    if (!id || isStreaming.value)
      return
    messages.value.push({ role: 'user', content: message, images })
    isStreaming.value = true
    runStatus.value = 'refining'
    error.value = null

    let assistantContent = ''
    let insideOptionsBlock = false
    let pendingOptions: string[] = []
    const assistantIdx = messages.value.push({ role: 'assistant', content: '' }) - 1

    abortController = new AbortController()
    try {
      const res = await fetch(`/api/refine/${id}/turn`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message, ...(images?.length ? { images } : {}) }),
        signal: abortController.signal,
      })

      if (!res.ok || !res.body) {
        error.value = `Server error: ${res.status}`
        messages.value.splice(assistantIdx, 1)
        return
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done)
          break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts.pop() ?? ''

        for (const part of parts) {
          const lines = part.split('\n')
          const eventLine = lines.find(l => l.startsWith('event:'))
          const event = eventLine ? eventLine.slice(7) : 'message'

          // The backend emits ONE `data:` line per SOURCE line (bufio.Scanner
          // strips the trailing newline; multi-line chunks become several
          // `data:` lines in one frame). Process EVERY data line and re-insert
          // the line breaks the wire dropped — otherwise headings/tables/lists
          // collapse into a single unformatted paragraph.
          for (const dataLine of lines) {
            if (!dataLine.startsWith('data:'))
              continue
            // Strip "data:" + exactly one optional SSE space — keep any further
            // leading whitespace so markdown indentation (nested lists/code) survives.
            let raw = dataLine.slice(5)
            if (raw.startsWith(' '))
              raw = raw.slice(1)

            // Phase marker: record progress, never show it in the message.
            const phaseMatch = raw.match(PHASE_DONE_LINE_RE)
            if (phaseMatch && PHASE_LABELS[phaseMatch[1]]) {
              const phase = phaseMatch[1]
              completedPhases.value.add(phase)
              messages.value[assistantIdx].phase = phase
              if (phase === 'approval')
                approvalReady.value = true
              // A line that is purely a marker contributes no text (and no break).
              if (raw.replace(PHASE_DONE_LINE_RE, '').trim() === '')
                continue
            }

            // Options block: collect lines between __options_start / __options_end.
            // Strip entirely from the displayed content — the options surface as
            // structured data on the message, not as visible text.
            if (raw.trim() === OPTIONS_START_LINE) {
              insideOptionsBlock = true
              pendingOptions = []
              continue
            }
            if (raw.trim() === OPTIONS_END_LINE) {
              insideOptionsBlock = false
              if (pendingOptions.length > 0) {
                messages.value[assistantIdx].options = pendingOptions.slice(0, 3)
              }
              continue
            }
            if (insideOptionsBlock) {
              const trimmed = raw.trim()
              if (trimmed)
                pendingOptions.push(trimmed)
              continue
            }

            // Default claude output is plain text, not JSON — fall back to treating
            // the raw line as assistant text when it does not parse as a JSON frame.
            let data: any
            try {
              data = JSON.parse(raw)
              if (typeof data !== 'object' || data === null)
                data = { text: raw }
            }
            catch {
              data = { text: raw }
            }

            if (event === 'phase_change' && data.phase) {
              completedPhases.value.add(data.phase)
              messages.value[assistantIdx].phase = data.phase
              if (data.phase === 'approval')
                approvalReady.value = true
            }
            else if (typeof data.text === 'string') {
              // Guard: never let a marker leak into displayed content. Empty text
              // (a blank source line) is kept so paragraph breaks are preserved.
              const text = String(data.text).replace(PHASE_DONE_LINE_RE, '')
              assistantContent += assistantContent ? `\n${text}` : text
              messages.value[assistantIdx].content = assistantContent
            }
          }
        }
      }
      // Stream finished cleanly → the run is complete.
      runStatus.value = 'draft_ready'
    }
    catch (err) {
      // An abort (modal closed) is expected — the detached run keeps going and
      // persists server-side; do not surface it as an error or a terminal status.
      if (!(err instanceof DOMException && err.name === 'AbortError'))
        error.value = String(err)
    }
    finally {
      isStreaming.value = false
      abortController = null
    }
  }

  // ── Detached-run status sync ──────────────────────────────────────────────
  function stopPolling(): void {
    if (pollTimer) {
      clearTimeout(pollTimer)
      pollTimer = null
    }
  }

  async function fetchStatus(): Promise<RefineRunStatus> {
    const id = taskId()
    if (!id)
      return 'none'
    try {
      const res = await fetch(`/api/refine/${id}/status`)
      if (!res.ok)
        return 'none'
      const data = await res.json() as { status?: RefineRunStatus, error?: string }
      if (data.error)
        error.value = data.error
      return data.status ?? 'none'
    }
    catch {
      return 'none'
    }
  }

  async function pollUntilDone(id: string): Promise<void> {
    // Abandon if the modal switched to a different task in the meantime.
    if (taskId() !== id)
      return
    const status = await fetchStatus()
    runStatus.value = status
    if (status === 'refining') {
      pollTimer = setTimeout(() => void pollUntilDone(id), POLL_INTERVAL_MS)
      return
    }
    // done / failed / idle → drop the indicator and pull the now-persisted turn.
    isStreaming.value = false
    if (status !== 'failed')
      await loadHistory()
  }

  /**
   * Reflect a detached run that may still be in flight when the modal (re)opens.
   * If the server reports `running`, switch on the working indicator and poll
   * until the run finishes, then reload the persisted turns. No-op while a live
   * POST stream is already driving the UI.
   */
  async function syncStatus(): Promise<void> {
    const id = taskId()
    if (!id || isStreaming.value)
      return
    const status = await fetchStatus()
    runStatus.value = status
    if (status === 'refining') {
      isStreaming.value = true
      stopPolling()
      pollTimer = setTimeout(() => void pollUntilDone(id), POLL_INTERVAL_MS)
    }
  }

  /** Abort an in-flight stream + stop status polling (e.g. modal closed). */
  function stop(): void {
    stopPolling()
    if (abortController) {
      abortController.abort()
      abortController = null
    }
    isStreaming.value = false
  }

  async function confirm(): Promise<PipelineTask | null> {
    const id = taskId()
    if (!id)
      return null
    try {
      const res = await fetch(actionEndpoint('approve_spec', id)!, { method: 'POST' })
      if (!res.ok) {
        error.value = (await res.json().catch(() => null))?.error ?? 'Confirm failed'
        return null
      }
      return await res.json()
    }
    catch (err) {
      error.value = String(err)
      return null
    }
  }

  function phaseLabel(phase: string) {
    return PHASE_LABELS[phase] ?? phase
  }

  // NOTE: there is intentionally no `watch(taskId)` reset here. Resetting shared
  // state on the id change races with the create flow (the null→newId transition
  // fires AFTER sendMessage has synchronously set isStreaming/runStatus, clobbering
  // them). Instead `loadHistory` resets phase state self-contained, and the
  // component drives loadHistory / syncStatus / stop at the right lifecycle points.

  return {
    messages,
    isStreaming,
    error,
    approvalReady,
    completedPhases,
    runStatus,
    syncStatus,
    stop,
    loadHistory,
    sendMessage,
    confirm,
    phaseLabel,
  }
}
