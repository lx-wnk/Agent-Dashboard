import { ref } from 'vue'
import type { Agent } from '../types'

export function useAgentPrompt(getAgent: () => Agent | null) {
  const promptInput = ref('')
  const isSending = ref(false)
  const sendStatus = ref<'sent' | 'error' | null>(null)
  const sendError = ref('')

  async function handleSend() {
    const agent = getAgent()
    const msg = promptInput.value.trim()
    if (!msg || isSending.value || !agent) return

    isSending.value = true
    sendStatus.value = null

    try {
      if (agent.channelAvailable && agent.status !== 'idle') {
        const res = await fetch(`/api/agents/${agent.sessionId}/message`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ message: msg }),
        })
        if (!res.ok) throw new Error((await res.json()).error || 'Send failed')
      } else {
        const res = await fetch('/api/agents/spawn', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            prompt: msg,
            cwd: agent.cwd,
            resumeSessionId: agent.sessionId,
          }),
        })
        if (!res.ok) throw new Error((await res.json()).error || 'Resume failed')
      }
      sendStatus.value = 'sent'
      promptInput.value = ''
    } catch (err) {
      sendStatus.value = 'error'
      sendError.value = err instanceof Error ? err.message : 'Failed'
    } finally {
      isSending.value = false
      setTimeout(() => { sendStatus.value = null }, 3000)
    }
  }

  return { promptInput, isSending, sendStatus, sendError, handleSend }
}
