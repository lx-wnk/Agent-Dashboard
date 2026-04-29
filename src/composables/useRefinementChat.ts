import type { PipelineTask } from '../types'
import { onUnmounted, ref, watch } from 'vue'

export interface ImageAttachment {
  dataUrl: string
  mimeType: string
}

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  phase?: string | null
  images?: ImageAttachment[]
}

const PHASE_LABELS: Record<string, string> = {
  analyse: 'Analyse',
  spec: 'Spec',
  umsetzungskonzept: 'Umsetzungskonzept',
  approval: 'Approval',
}

export function useRefinementChat(taskId: () => string | null) {
  const messages = ref<ChatMessage[]>([])
  const completedPhases = ref<Set<string>>(new Set())
  const isStreaming = ref(false)
  const error = ref<string | null>(null)
  const approvalReady = ref(false)
  const pollingTimer = ref<ReturnType<typeof setTimeout> | null>(null)

  function stopPolling() {
    if (pollingTimer.value !== null) {
      clearTimeout(pollingTimer.value)
      pollingTimer.value = null
    }
  }

  interface TurnsResponse {
    turns: Array<{ role: string, content: string, phase: string | null }>
    isProcessing: boolean
  }

  function applyTurnsToMessages(turns: TurnsResponse['turns']) {
    messages.value = turns.map(t => ({
      role: t.role as 'user' | 'assistant',
      content: t.content,
      phase: t.phase,
    }))
    for (const t of turns) {
      if (t.phase) completedPhases.value.add(t.phase)
    }
    if (completedPhases.value.has('approval')) approvalReady.value = true
  }

  function startPolling(id: string) {
    if (pollingTimer.value !== null) return

    const poll = async () => {
      try {
        const res = await fetch(`/api/refine/${id}/turns`)
        if (!res.ok) {
          pollingTimer.value = setTimeout(poll, 3000)
          return
        }
        const data = await res.json() as TurnsResponse
        if (data.isProcessing) {
          pollingTimer.value = setTimeout(poll, 3000)
          return
        }
        // Done — replace messages with completed turns and stop polling
        isStreaming.value = false
        applyTurnsToMessages(data.turns)
        stopPolling()
      }
      catch {
        pollingTimer.value = setTimeout(poll, 3000)
      }
    }

    pollingTimer.value = setTimeout(poll, 3000)
  }

  async function loadHistory() {
    const id = taskId()
    if (!id) return
    // If an SSE stream is active in this tab, don't overwrite live state
    if (isStreaming.value) return
    try {
      const res = await fetch(`/api/refine/${id}/turns`)
      if (!res.ok) {
        error.value = 'Failed to load history'
        return
      }
      const data = await res.json() as TurnsResponse
      applyTurnsToMessages(data.turns)

      if (data.isProcessing && !isStreaming.value) {
        isStreaming.value = true
        messages.value.push({ role: 'assistant', content: '' })
        startPolling(id)
      }
    }
    catch {
      error.value = 'Failed to load history'
    }
  }

  async function sendMessage(message: string, images?: ImageAttachment[]) {
    const id = taskId()
    if (!id || isStreaming.value) return
    messages.value.push({ role: 'user', content: message, images })
    isStreaming.value = true
    error.value = null

    let assistantContent = ''
    const assistantIdx = messages.value.push({ role: 'assistant', content: '' }) - 1

    try {
      const res = await fetch(`/api/refine/${id}/turn`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message, ...(images?.length ? { images } : {}) }),
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
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts.pop() ?? ''

        for (const part of parts) {
          const lines = part.split('\n')
          const eventLine = lines.find(l => l.startsWith('event:'))
          const dataLine = lines.find(l => l.startsWith('data:'))
          if (!dataLine) continue
          let data: any
          try {
            data = JSON.parse(dataLine.slice(5).trimStart())
          }
          catch {
            continue
          }
          const event = eventLine ? eventLine.slice(7) : 'message'

          if (event === 'phase_change' && data.phase) {
            completedPhases.value.add(data.phase)
            messages.value[assistantIdx].phase = data.phase
            if (data.phase === 'approval') approvalReady.value = true
          }
          else if (data.text) {
            assistantContent += data.text
            messages.value[assistantIdx].content = assistantContent
          }
        }
      }
    }
    catch (err) {
      error.value = String(err)
    }
    finally {
      isStreaming.value = false
    }
  }

  async function confirm(): Promise<PipelineTask | null> {
    const id = taskId()
    if (!id) return null
    try {
      const res = await fetch(`/api/refine/${id}/confirm`, { method: 'POST' })
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

  watch(taskId, () => {
    stopPolling()
    messages.value = []
    completedPhases.value = new Set()
    isStreaming.value = false
    approvalReady.value = false
    error.value = null
  })

  onUnmounted(() => {
    stopPolling()
  })

  return {
    messages,
    completedPhases,
    isStreaming,
    error,
    approvalReady,
    loadHistory,
    sendMessage,
    confirm,
    phaseLabel,
  }
}
