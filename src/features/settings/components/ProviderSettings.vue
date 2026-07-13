<script setup lang="ts">
import { ref } from 'vue'
import { toast } from '@/composables/useToast'
import { useProviderSettings } from '@/features/settings/composables/useProviderSettings'
import { errorMessage } from '@/utils/errorMessage'

const { providers, loading, toggle } = useProviderSettings()
const saving = ref<string | null>(null)

async function handleToggle(id: string, next: boolean) {
  saving.value = id
  try {
    await toggle(id, next)
  }
  catch (e) {
    toast.error(errorMessage(e, 'Toggle failed'))
  }
  finally {
    saving.value = null
  }
}
</script>

<template>
  <div class="space-y-6">
    <div>
      <h3 class="text-sm font-semibold text-fg-soft">
        Agent providers
      </h3>
      <p class="text-xs text-fg-mute mt-0.5">
        Enable monitoring for additional coding-agent CLIs. Off by default — a provider's
        sessions are only read while it is enabled. Claude is always monitored.
      </p>
    </div>

    <div role="status" aria-live="polite" aria-atomic="true" class="text-xs text-fg-faint" :class="{ 'sr-only': !loading }">
      {{ loading ? 'Loading…' : '' }}
    </div>

    <ul v-if="!loading" class="border border-line rounded-lg divide-y divide-line text-xs">
      <li
        v-for="p in providers"
        :key="p.id"
        class="flex items-center justify-between gap-3 px-3 py-2.5"
        :class="{ 'opacity-50': !p.enabled }"
      >
        <div>
          <p class="font-medium text-fg">
            {{ p.displayName }}
          </p>
          <p v-if="!p.configDirPresent" class="text-fg-faint text-[10px] mt-0.5">
            Config dir not found — start the agent once.
          </p>
        </div>
        <button
          type="button"
          role="switch"
          :aria-checked="p.enabled"
          :aria-label="`Monitor ${p.displayName} sessions`"
          class="relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 dark:focus-visible:ring-offset-slate-900 disabled:cursor-not-allowed"
          :class="p.enabled ? 'bg-accent' : 'bg-line-strong'"
          :disabled="saving === p.id"
          @click="handleToggle(p.id, !p.enabled)"
        >
          <span
            class="pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform"
            :class="p.enabled ? 'translate-x-5' : 'translate-x-0'"
          />
        </button>
      </li>
    </ul>
  </div>
</template>
