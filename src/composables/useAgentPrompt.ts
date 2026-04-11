import type { Agent, OutputMessage } from '../types'
import { ref } from 'vue'

export type OnMessageSent = (msg: OutputMessage) => void

export function useAgentPrompt(getAgent: () => Agent | null, onMessageSent?: OnMessageSent) {
  const promptInput = ref('')
  const isSending = ref(false)
  const sendStatus = ref<'sent' | 'error' | null>(null)
  const sendError = ref('')

  async function handleSend() {
    const agent = getAgent()
    const msg = promptInput.value.trim()
    if (!msg || isSending.value || !agent)
      return

    isSending.value = true
    sendStatus.value = null

    try {
      if (agent.channelAvailable && agent.status !== 'idle') {
        const res = await fetch(`/api/agents/${agent.sessionId}/message`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ message: msg }),
        })
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          throw new Error(data.error || `Send failed (${res.status})`)
        }
      }
      else {
        const res = await fetch('/api/agents/spawn', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            prompt: msg,
            cwd: agent.cwd,
            resumeSessionId: agent.sessionId,
          }),
        })
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          throw new Error(data.error || `Resume failed (${res.status})`)
        }
      }
      sendStatus.value = 'sent'
      onMessageSent?.({
        role: 'human',
        content: msg,
        timestamp: new Date().toISOString(),
      })
      promptInput.value = ''
    }
    catch (err) {
      sendStatus.value = 'error'
      sendError.value = err instanceof Error ? err.message : 'Failed'
    }
    finally {
      isSending.value = false
      setTimeout(() => {
        sendStatus.value = null
      }, 3000)
    }
  }

  return { promptInput, isSending, sendStatus, sendError, handleSend }
}
