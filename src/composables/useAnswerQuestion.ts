import { ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'
import { SEND_STATUS_RESET_MS } from '../utils/timing'

export function useAnswerQuestion() {
  const isSending = ref(false)
  const sendStatus = ref<'sent' | 'error' | null>(null)
  const sendError = ref('')

  async function submit(
    pid: number,
    toolUseId: string,
    answers: { header: string, selected: string[], customText?: string }[],
    chatText?: string,
  ): Promise<boolean> {
    isSending.value = true
    sendStatus.value = null
    sendError.value = ''

    try {
      const res = await fetch(`/api/agents/${pid}/answer-question`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ toolUseId, answers, ...(chatText ? { chatText } : {}) }),
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok)
        throw new Error(data.error || `Send failed (${res.status})`)
      sendStatus.value = 'sent'
      return true
    }
    catch (err) {
      sendStatus.value = 'error'
      sendError.value = errorMessage(err, 'Failed to send answer')
      return false
    }
    finally {
      isSending.value = false
      setTimeout(() => {
        sendStatus.value = null
      }, SEND_STATUS_RESET_MS)
    }
  }

  return { isSending, sendStatus, sendError, submit }
}
