import type { PipelineTask } from '../types'
import { ref } from 'vue'

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
  phase?: string | null
}

export type RefinementPhase = 'analyse' | 'spec' | 'umsetzungskonzept' | 'approval' | null

const PHASE_LABELS: Record<string, string> = {
  analyse: 'Analyse',
  spec: 'Spec',
  umsetzungskonzept: 'Umsetzungskonzept',
  approval: 'Approval',
}

export function useRefinementChat(taskId: () => string | null) {
  const messages = ref<ChatMessage[]>([])
  const currentPhase = ref<RefinementPhase>(null)
  const completedPhases = ref<Set<string>>(new Set())
  const isStreaming = ref(false)
  const error = ref<string | null>(null)
  const approvalReady = ref(false)

  async function loadHistory() {
    const id = taskId()
    if (!id) return
    try {
      const res = await fetch(`/api/refine/${id}/turns`)
      const turns = await res.json() as Array<{ role: string, content: string, phase: string | null }>
      messages.value = turns.map(t => ({ role: t.role as 'user' | 'assistant', content: t.content, phase: t.phase }))
      for (const t of turns) {
        if (t.phase) completedPhases.value.add(t.phase)
      }
      if (completedPhases.value.has('approval')) approvalReady.value = true
    }
    catch {
      error.value = 'Failed to load history'
    }
  }

  async function sendMessage(message: string) {
    const id = taskId()
    if (!id || isStreaming.value) return
    messages.value.push({ role: 'user', content: message })
    isStreaming.value = true
    error.value = null

    let assistantContent = ''
    const assistantIdx = messages.value.push({ role: 'assistant', content: '' }) - 1

    try {
      const res = await fetch(`/api/refine/${id}/turn`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message }),
      })

      const reader = res.body!.getReader()
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
          const data = JSON.parse(dataLine.slice(5))
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
    const res = await fetch(`/api/refine/${id}/confirm`, { method: 'POST' })
    if (!res.ok) {
      error.value = (await res.json()).error
      return null
    }
    return res.json()
  }

  function phaseLabel(phase: string) {
    return PHASE_LABELS[phase] ?? phase
  }

  return {
    messages,
    currentPhase,
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
