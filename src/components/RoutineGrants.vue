<script setup lang="ts">
import { computed, ref } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import { toast } from '@/composables/useToast'
import { useGrants } from '@/features/settings/composables/useGrants'
import { errorMessage } from '@/utils/errorMessage'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  scheduleId: string
}>()

const { grants, loading, error, revokeGrant } = useGrants()

const routineGrants = computed(() =>
  grants.value.filter(g => g.contextKind === 'routine' && g.contextRef === props.scheduleId && !g.revokedAt),
)

const confirmRevokeId = ref<string | null>(null)
const revokingId = ref<string | null>(null)

async function handleRevoke(id: string) {
  revokingId.value = id
  try {
    await revokeGrant(id)
    confirmRevokeId.value = null
  }
  catch (e) {
    toast.error(errorMessage(e))
  }
  finally {
    revokingId.value = null
  }
}
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <h4 class="text-xs font-medium text-fg-soft">
      Saved decisions
    </h4>
    <p v-if="loading" class="text-xs text-fg-faint">
      Loading decisions…
    </p>
    <p v-else-if="error" role="alert" class="text-xs text-danger-text">
      {{ error }}
    </p>
    <p v-else-if="routineGrants.length === 0" class="text-xs text-fg-faint">
      No decisions saved for this routine
    </p>
    <ul v-else class="flex flex-col divide-y divide-line text-xs">
      <li
        v-for="g in routineGrants"
        :key="g.id"
        :data-testid="`routine-grant-${g.id}`"
        class="flex flex-wrap items-center gap-x-3 gap-y-1 py-1.5"
      >
        <span class="font-mono text-fg">{{ g.capabilityName }}</span>
        <span
          v-if="g.mode === 'allow'"
          class="px-1.5 py-0.5 rounded-full bg-green-900/40 text-green-400"
        >Allowed</span>
        <span
          v-else
          class="px-1.5 py-0.5 rounded-full bg-red-900/40 text-red-400"
        >Denied</span>
        <span class="text-fg-faint">{{ formatDateTime(g.grantedAt) }}</span>
        <template v-if="confirmRevokeId === g.id">
          <AppButton variant="danger" size="sm" :disabled="revokingId === g.id" :data-testid="`routine-grant-revoke-confirm-${g.id}`" @click="handleRevoke(g.id)">
            {{ revokingId === g.id ? 'Revoking…' : 'Confirm' }}
          </AppButton>
          <AppButton variant="secondary" size="sm" :data-testid="`routine-grant-revoke-cancel-${g.id}`" @click="confirmRevokeId = null">
            Cancel
          </AppButton>
        </template>
        <button
          v-else
          type="button"
          class="bg-transparent border-none text-fg-mute cursor-pointer text-xs px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400"
          :data-testid="`routine-grant-revoke-${g.id}`"
          @click="confirmRevokeId = g.id"
        >
          Revoke
        </button>
      </li>
    </ul>
  </div>
</template>
