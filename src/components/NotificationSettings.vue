<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import AppButton from './ui/AppButton.vue'

interface NotifPref {
  eventType: string
  channels: string[]
  enabled: boolean
}

const KNOWN_EVENTS: { type: string; label: string; description: string }[] = [
  { type: 'on_hold', label: 'On Hold', description: 'Task paused — requires user input' },
  { type: 'approval_needed', label: 'Approval Needed', description: 'Stage agent requesting tool permission' },
  { type: 'completed', label: 'Completed', description: 'Task finished successfully' },
  { type: 'failed', label: 'Failed', description: 'Task or stage encountered an unrecoverable error' },
  { type: 'budget_exceeded', label: 'Budget Exceeded', description: 'Token or cost budget threshold reached' },
  { type: 'iteration_warning', label: 'Iteration Warning', description: 'Stage nearing retry limit' },
]

const CHANNELS = ['webhook', 'email', 'browser', 'system'] as const
type Channel = typeof CHANNELS[number]

// F015 — use Record instead of Map for reactive correctness
const prefs = ref<Record<string, NotifPref>>({})
const config = ref<Record<string, string>>({})
const loading = ref(true)
const error = ref<string | null>(null)

const savingPref = ref<string | null>(null)
const savingConfig = ref(false)
const configSaveOk = ref(false)
const prefSaveOk = ref<string | null>(null)

// F054 — track timer IDs for cleanup on unmount
let prefSaveOkTimer: ReturnType<typeof setTimeout> | null = null
let configSaveOkTimer: ReturnType<typeof setTimeout> | null = null

onBeforeUnmount(() => {
  if (prefSaveOkTimer) clearTimeout(prefSaveOkTimer)
  if (configSaveOkTimer) clearTimeout(configSaveOkTimer)
})

function getPref(eventType: string): NotifPref {
  // F015 — object property access instead of Map.get
  return prefs.value[eventType] ?? { eventType, channels: [], enabled: false }
}

onMounted(async () => {
  try {
    const [prefRes, cfgRes] = await Promise.all([
      fetch('/api/notifications/preferences'),
      fetch('/api/notifications/config'),
    ])
    // F047 — preserve which endpoint failed in the error message
    if (!prefRes.ok || !cfgRes.ok)
      throw new Error(`Failed to load notification settings (HTTP ${prefRes.ok ? cfgRes.status : prefRes.status})`)

    const prefList: NotifPref[] = await prefRes.json()
    // F015 — build object from array instead of Map.set loop
    prefs.value = prefList.reduce<Record<string, NotifPref>>((acc, p) => {
      acc[p.eventType] = p
      return acc
    }, {})

    config.value = await cfgRes.json()
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load notifications'
  }
  finally {
    loading.value = false
  }
})

async function toggleEnabled(eventType: string) {
  const current = getPref(eventType)
  await savePref(eventType, { ...current, enabled: !current.enabled })
}

async function toggleChannel(eventType: string, channel: Channel) {
  const current = getPref(eventType)
  const channels = current.channels.includes(channel)
    ? current.channels.filter(c => c !== channel)
    : [...current.channels, channel]
  await savePref(eventType, { ...current, channels })
}

async function savePref(eventType: string, updated: NotifPref) {
  savingPref.value = eventType
  prefSaveOk.value = null
  try {
    const res = await fetch(`/api/notifications/preferences/${eventType}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ channels: updated.channels, enabled: updated.enabled }),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const saved: NotifPref = await res.json()
    // F015 — object spread assignment instead of Map.set
    prefs.value = { ...prefs.value, [eventType]: saved }
    prefSaveOk.value = eventType
    // F054 — store timer ID for cleanup
    prefSaveOkTimer = setTimeout(() => {
      if (prefSaveOk.value === eventType) prefSaveOk.value = null
    }, 1500)
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : 'Save failed'
  }
  finally {
    savingPref.value = null
  }
}

async function saveConfig() {
  savingConfig.value = true
  configSaveOk.value = false
  error.value = null
  try {
    const res = await fetch('/api/notifications/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config.value),
    })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    config.value = await res.json()
    configSaveOk.value = true
    // F054 — store timer ID for cleanup
    configSaveOkTimer = setTimeout(() => { configSaveOk.value = false }, 2000)
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : 'Save failed'
  }
  finally {
    savingConfig.value = false
  }
}
</script>

<template>
  <div class="space-y-6">
    <div>
      <h3 class="text-sm font-semibold text-fg-soft">
        Notifications
      </h3>
      <p class="text-xs text-fg-mute mt-0.5">
        Configure which pipeline events trigger notifications and via which channels.
      </p>
    </div>

    <!-- F011 — role="status"/role="alert" rendered unconditionally so announcements
         fire when content changes (ARIA live regions must exist before content mutates). -->
    <div role="status" aria-live="polite" aria-atomic="true" class="text-xs text-slate-400" :class="{ 'sr-only': !loading }">
      {{ loading ? 'Loading…' : '' }}
    </div>
    <div role="alert" aria-atomic="true" class="text-xs text-red-500" :class="{ 'sr-only': !error || loading }">
      {{ !loading ? (error ?? '') : '' }}
    </div>

    <template v-if="!loading">
      <!-- F012 — screen-reader-only live region for pref save confirmation -->
      <div role="status" aria-live="polite" aria-atomic="true" class="sr-only">
        {{ prefSaveOk ? `${prefSaveOk} preference saved` : '' }}
      </div>

      <!-- Event preferences table — F039: overflow-x-auto for narrow viewports -->
      <div class="border border-line rounded-lg overflow-x-auto text-xs">
        <table class="w-full">
          <thead>
            <tr class="bg-raised/50">
              <!-- F004 / F018 — scope="col" on all column headers -->
              <th
                scope="col"
                class="text-left text-[10px] uppercase tracking-wide text-fg-faint px-3 py-2 font-medium"
              >
                Event
              </th>
              <th
                scope="col"
                class="text-center text-[10px] uppercase tracking-wide text-fg-faint px-3 py-2 font-medium"
              >
                Enabled
              </th>
              <th
                v-for="ch in CHANNELS"
                :key="ch"
                scope="col"
                class="text-center text-[10px] uppercase tracking-wide text-fg-faint px-2 py-2 font-medium capitalize"
              >
                {{ ch }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="ev in KNOWN_EVENTS"
              :key="ev.type"
              class="border-t border-line"
              :class="{ 'opacity-50': !getPref(ev.type).enabled }"
              :aria-disabled="!getPref(ev.type).enabled"
            >
              <!-- F004 / F018 — row header for the event label -->
              <th scope="row" class="px-3 py-2.5 text-left font-normal">
                <p class="font-medium text-fg">
                  {{ ev.label }}
                </p>
                <p class="text-fg-faint text-[10px] mt-0.5">
                  {{ ev.description }}
                </p>
              </th>
              <td class="px-3 py-2.5 text-center">
                <!-- F001 — role="switch" + aria-checked + aria-label -->
                <!-- F002 — replace focus:outline-none with focus-visible:ring-* -->
                <!-- F003 — h-6 w-11 for adequate touch target (24×44px) -->
                <button
                  type="button"
                  role="switch"
                  :aria-checked="getPref(ev.type).enabled"
                  :aria-label="`${ev.label} notifications`"
                  class="relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-slate-900"
                  :class="getPref(ev.type).enabled ? 'bg-blue-500' : 'bg-slate-300 dark:bg-slate-600'"
                  :disabled="savingPref === ev.type"
                  @click="toggleEnabled(ev.type)"
                >
                  <!-- F003 — inner thumb h-4 w-4, translate-x-5 when on -->
                  <span
                    class="pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform"
                    :class="getPref(ev.type).enabled ? 'translate-x-5' : 'translate-x-0'"
                  />
                </button>
              </td>
              <td
                v-for="ch in CHANNELS"
                :key="ch"
                class="px-2 py-2.5 text-center"
              >
                <!-- F004 — aria-label on each checkbox for full context -->
                <input
                  type="checkbox"
                  class="cursor-pointer accent-blue-500"
                  :aria-label="`Send ${ev.label} notifications via ${ch}`"
                  :checked="getPref(ev.type).channels.includes(ch)"
                  :disabled="!getPref(ev.type).enabled || savingPref === ev.type"
                  @change="toggleChannel(ev.type, ch)"
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- F043 — auto-save hint below channel table -->
      <p class="text-[10px] text-fg-faint mt-1">
        Channel changes save automatically.
      </p>

      <!-- Delivery config -->
      <div class="space-y-3">
        <h4 class="text-xs font-semibold text-fg-mute uppercase tracking-wide">
          Delivery Configuration
        </h4>
        <!-- F042 — wrap inputs + save button in a form for Enter-key submission + native validation -->
        <form class="space-y-2" @submit.prevent="saveConfig">
          <div class="flex flex-col gap-1">
            <label class="text-xs text-fg-mute">Webhook URL</label>
            <input
              v-model="config['webhook_url']"
              type="url"
              placeholder="https://hooks.example.com/..."
              class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-xs text-fg focus:outline-none focus:border-blue-500 font-mono"
            >
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-xs text-fg-mute">Email recipient</label>
            <input
              v-model="config['email_to']"
              type="email"
              placeholder="you@example.com"
              class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-xs text-fg focus:outline-none focus:border-blue-500"
            >
          </div>
          <div class="flex items-center gap-2">
            <!-- F042 — type="submit" so Enter key and form submission work -->
            <AppButton type="submit" size="sm" :disabled="savingConfig">
              {{ savingConfig ? 'Saving…' : configSaveOk ? 'Saved!' : 'Save Config' }}
            </AppButton>
            <!-- F012 — aria-live rendered unconditionally for reliable announcement -->
            <p role="status" aria-live="polite" aria-atomic="true" class="text-xs text-green-600 dark:text-green-400" :class="{ 'sr-only': !configSaveOk }">
              {{ configSaveOk ? 'Settings saved.' : '' }}
            </p>
          </div>
        </form>
      </div>
    </template>
  </div>
</template>
