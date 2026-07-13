<script setup lang="ts">
import type { SessionInfo } from '../../composables/useSessions'
import { computed, ref, watch } from 'vue'
import { useClipboardCopy } from '../../composables/useCopyId'
import { useOnboarding } from '../../composables/useOnboarding'
import { useServerConfig } from '../../composables/useServerConfig'
import { useSessions } from '../../composables/useSessions'
import { errorMessage } from '../../utils/errorMessage'
import { buildMcpAddCommand } from '../../utils/mcpCommand'
import ChannelScriptCallout from '../shell/ChannelScriptCallout.vue'
import AppButton from '../ui/AppButton.vue'
import AppModal from '../ui/AppModal.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [], spawned: [pid: number] }>()

// Native installer is Anthropic's current recommendation (superseded `npm install -g
// @anthropic-ai/claude-code`); macOS/Linux only, matching this project's platform support.
const CLI_INSTALL_COMMAND = 'curl -fsSL https://claude.ai/install.sh | bash'

const { status, error: statusError, fetchStatus, registerMcp, complete } = useOnboarding()
const { mcpServerName, mcpEndpoint, loadServerConfig } = useServerConfig()
const { sessions, loading: sessionsLoading, refetch: loadSessions } = useSessions()

const controllableSessions = computed(() => sessions.value.filter(s => !s.isRunning))

const rechecking = ref(false)
const connecting = ref(false)
const connectError = ref('')
const registeredCommand = ref('')
const connectingSessionId = ref<string | null>(null)
const sessionError = ref('')
const copiedKey = ref<'cli-install' | 'mcp-manual' | null>(null)

// buildMcpAddCommand validates its args and throws on an empty endpoint — guards the
// window between mcpServerName and mcpEndpoint arriving from /api/config.
function previewMcpAddCommand(): string {
  if (!mcpServerName.value)
    return ''
  try {
    return buildMcpAddCommand(window.location.origin, '<your-api-token>', mcpServerName.value, mcpEndpoint.value)
  }
  catch {
    return ''
  }
}

const manualMcpCommand = computed(() => registeredCommand.value || previewMcpAddCommand())

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    fetchStatus()
    loadServerConfig()
    loadSessions()
  }
}, { immediate: true })

async function recheck() {
  rechecking.value = true
  await fetchStatus()
  rechecking.value = false
}

async function onConnectMcp() {
  connecting.value = true
  connectError.value = ''
  const result = await registerMcp()
  connecting.value = false
  if (!result) {
    connectError.value = statusError.value ?? 'Failed to connect the dashboard.'
    return
  }
  registeredCommand.value = result.command
  if (!result.ok)
    connectError.value = 'Could not connect automatically — run the command below manually.'
}

const { copy: copyToClipboard } = useClipboardCopy()

async function copyText(key: NonNullable<typeof copiedKey.value>, value: string) {
  try {
    await copyToClipboard(value)
    copiedKey.value = key
    setTimeout(() => {
      if (copiedKey.value === key)
        copiedKey.value = null
    }, 2000)
  }
  catch {
    // Clipboard API unavailable (permissions/insecure context) — text stays selectable.
  }
}

async function connectSession(session: SessionInfo) {
  connectingSessionId.value = session.sessionId
  sessionError.value = ''
  try {
    const res = await fetch('/api/agents/spawn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        cwd: session.projectPath,
        prompt: 'continue',
        resumeSessionId: session.sessionId,
        enableChannel: true,
      }),
    })
    const data = await res.json()
    if (!res.ok)
      throw new Error(data.error || 'Failed to connect session')
    await complete()
    emit('spawned', data.pid as number)
    emit('close')
  }
  catch (err: unknown) {
    sessionError.value = errorMessage(err, 'Failed to connect session')
  }
  finally {
    connectingSessionId.value = null
  }
}

// Dismisses the overlay for this session without persisting completion — the
// flow reappears on next launch since onboarding.completed stays false server-side.
function skip() {
  emit('close')
}

async function finish() {
  await complete()
  emit('close')
}
</script>

<template>
  <AppModal :open="open" size="auto" labelled-by="onboarding-title" @close="skip">
    <div data-testid="onboarding-flow" class="bg-card border border-line rounded-2xl flex flex-col overflow-hidden shadow-2xl w-full" style="max-width: 640px; max-height: 88vh;">
      <header class="flex items-center justify-between px-5 py-4 border-b border-line shrink-0">
        <h2 id="onboarding-title" class="text-lg font-semibold text-fg">
          Let's get you set up
        </h2>
      </header>

      <div class="flex-1 min-h-0 overflow-y-auto p-5 flex flex-col gap-6">
        <!-- Step 1: Claude CLI -->
        <section data-testid="onboarding-step-cli">
          <h3 class="text-[13px] font-semibold uppercase tracking-wide text-fg-mute mb-2">
            1. Claude CLI
          </h3>
          <p v-if="status?.cliInstalled" class="text-sm text-green-600 dark:text-green-400 mb-2">
            &#10003; v{{ status.cliVersion }} detected
          </p>
          <template v-else>
            <p class="text-sm text-warning-text mb-2">
              Not found &mdash; install it, then re-check:
            </p>
            <div
              role="region"
              aria-label="CLI install command"
              class="relative font-mono text-xs bg-raised text-fg-soft p-3 pr-10 rounded border border-line mb-2 break-all"
            >
              {{ CLI_INSTALL_COMMAND }}
              <button
                type="button"
                class="absolute right-1.5 top-1.5 p-1.5 rounded hover:bg-app text-fg-mute hover:text-fg transition-colors"
                aria-label="Copy install command"
                @click="copyText('cli-install', CLI_INSTALL_COMMAND)"
              >
                <span aria-hidden="true" class="text-sm leading-none">{{ copiedKey === 'cli-install' ? '✓' : '⧉' }}</span>
              </button>
            </div>
          </template>
          <AppButton data-testid="onboarding-recheck" size="sm" variant="secondary" :disabled="rechecking" @click="recheck">
            {{ rechecking ? 'Checking...' : 'Re-check' }}
          </AppButton>
        </section>

        <!-- Step 2: Connect the dashboard -->
        <section data-testid="onboarding-step-mcp">
          <h3 class="text-[13px] font-semibold uppercase tracking-wide text-fg-mute mb-2">
            2. Connect the dashboard
          </h3>
          <div class="flex items-center gap-2 mb-2">
            <AppButton data-testid="onboarding-connect-mcp" variant="primary" :disabled="connecting" @click="onConnectMcp">
              {{ connecting ? 'Connecting...' : 'Connect the dashboard' }}
            </AppButton>
            <span v-if="status?.mcpRegistered" class="text-sm text-green-600 dark:text-green-400">&#10003; Connected</span>
          </div>
          <p v-if="connectError" role="alert" class="text-xs text-danger-text mb-2">
            {{ connectError }}
          </p>
          <p class="text-xs text-fg-mute mb-1">
            or run it yourself:
          </p>
          <div
            v-if="manualMcpCommand"
            role="region"
            aria-label="CLI command"
            class="relative font-mono text-xs bg-raised text-fg-soft p-3 pr-10 rounded border border-line break-all"
          >
            {{ manualMcpCommand }}
            <button
              type="button"
              class="absolute right-1.5 top-1.5 p-1.5 rounded hover:bg-app text-fg-mute hover:text-fg transition-colors"
              aria-label="Copy CLI command"
              @click="copyText('mcp-manual', manualMcpCommand)"
            >
              <span aria-hidden="true" class="text-sm leading-none">{{ copiedKey === 'mcp-manual' ? '✓' : '⧉' }}</span>
            </button>
          </div>
        </section>

        <!-- Step 3: make a session controllable -->
        <section data-testid="onboarding-step-session">
          <h3 class="text-[13px] font-semibold uppercase tracking-wide text-fg-mute mb-2">
            3. Make a session controllable
          </h3>
          <p v-if="sessionError" role="alert" class="text-xs text-danger-text mb-2">
            {{ sessionError }}
          </p>
          <p v-if="sessionsLoading" class="text-sm text-fg-mute">
            Loading sessions...
          </p>
          <div v-else-if="controllableSessions.length === 0" class="text-sm text-fg-mute">
            <p class="mb-2">
              No existing sessions found &mdash; start one from a terminal, then come back here.
            </p>
            <ChannelScriptCallout />
          </div>
          <ul v-else class="flex flex-col gap-2">
            <li
              v-for="s in controllableSessions"
              :key="s.sessionId"
              class="bg-app border border-line rounded-md px-3 py-2.5 flex items-center justify-between gap-3"
            >
              <div class="min-w-0">
                <p class="text-sm font-semibold text-fg truncate">
                  {{ s.firstPrompt ?? s.projectName }}
                </p>
                <code class="font-mono text-xs text-fg-mute truncate block">{{ s.projectPath }}</code>
              </div>
              <AppButton
                data-testid="onboarding-session-connect"
                size="sm"
                variant="secondary"
                :disabled="connectingSessionId === s.sessionId"
                @click="connectSession(s)"
              >
                {{ connectingSessionId === s.sessionId ? 'Connecting...' : 'Connect' }}
              </AppButton>
            </li>
          </ul>
        </section>
      </div>

      <footer class="flex justify-end gap-2 px-5 py-3 border-t border-line shrink-0">
        <AppButton data-testid="onboarding-skip" variant="secondary" @click="skip">
          Skip for now
        </AppButton>
        <AppButton data-testid="onboarding-done" variant="primary" @click="finish">
          Done
        </AppButton>
      </footer>
    </div>
  </AppModal>
</template>
