import type { Agent, OutputMessage } from '../types'
import { onUnmounted, ref } from 'vue'
import { addPending } from '../utils/pendingMessages'
import { BACKGROUND_SYNC_TAG } from '../utils/swConstants'
import { dispatchSlashCommand, parseSlashCommand, SLASH_COMMAND_DEFS } from './useSlashCommands'

export type OnMessageSent = (msg: OutputMessage) => void

export interface AgentPromptContext {
  taskId?: string
  cwd?: string
}

async function registerBackgroundSync(): Promise<void> {
  if (!('serviceWorker' in navigator) || !('SyncManager' in window))
    return
  try {
    const registration = await navigator.serviceWorker.ready
    // @ts-expect-error — SyncManager is not yet in all TypeScript lib versions
    await registration.sync.register(BACKGROUND_SYNC_TAG)
  }
  catch {
    // Background Sync not supported or SW not yet active — SSE-reconnect fallback handles this
  }
}

function isNetworkFailure(err: unknown): boolean {
  return err instanceof TypeError
}

export function useAgentPrompt(
  getAgent: () => Agent | null,
  onMessageSent?: OnMessageSent,
  ctx?: AgentPromptContext,
) {
  const promptInput = ref('')
  const isSending = ref(false)
  const sendStatus = ref<'sent' | 'error' | 'queued' | null>(null)
  const sendError = ref('')

  async function handleSend() {
    const agent = getAgent()
    const msg = promptInput.value.trim()
    if (!msg || isSending.value || !agent)
      return

    // Slash-command intercept: only known dashboard commands are intercepted;
    // unrecognised slash commands (e.g. /btw, /compact, /review) fall through to the agent.
    const parsed = parseSlashCommand(msg)
    if (parsed) {
      const [cmd, args] = parsed
      const isKnownCommand = SLASH_COMMAND_DEFS.some(def => def.name === cmd)
      if (isKnownCommand) {
        promptInput.value = ''
        isSending.value = true
        sendError.value = ''
        try {
          const result = await dispatchSlashCommand(cmd, args, {
            taskId: ctx?.taskId ?? agent?.pipelineTaskId,
            cwd: ctx?.cwd ?? agent?.cwd,
          })
          sendStatus.value = result.ok ? 'sent' : 'error'
          sendError.value = result.ok ? '' : result.message
          if (result.ok && result.message) {
            onMessageSent?.({ role: 'channel_reply', content: result.message, timestamp: new Date().toISOString() })
          }
        }
        finally {
          isSending.value = false
          setTimeout(() => {
            sendStatus.value = null
          }, 3000)
        }
        return
      }
    }

    isSending.value = true
    sendStatus.value = null

    // Optimistic: show message immediately, clear input, don't wait for network
    onMessageSent?.({ role: 'human', content: msg, timestamp: new Date().toISOString() })
    promptInput.value = ''

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
    }
    catch (err) {
      if (isNetworkFailure(err)) {
        const useChannel = !!(agent.channelAvailable && agent.status !== 'idle')
        try {
          await addPending({
            agentPid: agent.pid,
            sessionId: agent.sessionId,
            message: msg,
            timestamp: Date.now(),
            useChannel,
            cwd: agent.cwd,
          })
          await registerBackgroundSync()
          sendStatus.value = 'queued'
          sendError.value = 'Offline — message queued'
        }
        catch {
          sendStatus.value = 'error'
          sendError.value = 'Offline and could not queue message'
        }
      }
      else {
        sendStatus.value = 'error'
        sendError.value = err instanceof Error ? err.message : 'Failed'
      }
    }
    finally {
      isSending.value = false
      if (sendStatus.value !== 'queued') {
        setTimeout(() => {
          sendStatus.value = null
        }, 3000)
      }
    }
  }

  const onDrainSuccess = () => {
    if (sendStatus.value === 'queued')
      sendStatus.value = null
  }
  window.addEventListener('drain-success', onDrainSuccess)
  onUnmounted(() => window.removeEventListener('drain-success', onDrainSuccess))

  return { promptInput, isSending, sendStatus, sendError, handleSend }
}
