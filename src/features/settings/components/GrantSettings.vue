<script setup lang="ts">
import type { CreateGrantInput, GrantContextKind, GrantMode } from '@/features/settings/composables/useGrants'
import { computed, ref } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import { toast } from '@/composables/useToast'
import {
  GRANT_CONTEXT_GLOBAL,
  GRANT_CONTEXT_KINDS,
  GRANT_MODES,
  LEGACY_GRANTED_BY,
  useGrants,
} from '@/features/settings/composables/useGrants'
import { errorMessage } from '@/utils/errorMessage'

const { grants, capabilities, loading, fetchGrants, createGrant, revokeGrant } = useGrants()

// ── Capability filter ───────────────────────────────────────────────────────
const capabilityFilter = ref('')
const capabilityFilterOptions = computed(() => [
  { value: '', label: 'All capabilities' },
  ...capabilities.value.map(c => ({ value: c.name, label: c.name })),
])

function onFilterChange(value: string) {
  capabilityFilter.value = value
  void fetchGrants(value || undefined)
}

// ── Create form ──────────────────────────────────────────────────────────────
interface GrantFormState {
  capabilityName: string
  contextKind: GrantContextKind
  contextRef: string
  pattern: string
  mode: GrantMode
  limitCount: number
  limitWindowSeconds: number
  expiresInSeconds: number | null
  reason: string
}

function emptyForm(): GrantFormState {
  return {
    capabilityName: '',
    contextKind: 'global',
    contextRef: '',
    pattern: '',
    mode: 'allow',
    limitCount: 0,
    limitWindowSeconds: 0,
    expiresInSeconds: null,
    reason: '',
  }
}

const formVisible = ref(false)
const formSaving = ref(false)
const form = ref<GrantFormState>(emptyForm())

const capabilityOptions = computed(() => capabilities.value.map(c => ({ value: c.name, label: c.name })))
const contextKindOptions = computed(() => GRANT_CONTEXT_KINDS.map(k => ({ value: k, label: k })))
const modeOptions = computed(() => GRANT_MODES.map(m => ({ value: m, label: m })))

// The server refuses a ref on `global` and requires one on every other kind —
// clearing and disabling the field on `global` keeps that half of the rule
// unreachable from the form without re-implementing the server's check.
const contextRefDisabled = computed(() => form.value.contextKind === GRANT_CONTEXT_GLOBAL)
function onContextKindChange(kind: GrantContextKind) {
  form.value.contextKind = kind
  if (kind === GRANT_CONTEXT_GLOBAL)
    form.value.contextRef = ''
}

function openCreate() {
  form.value = emptyForm()
  formVisible.value = true
}

function closeForm() {
  formVisible.value = false
}

async function handleCreate() {
  if (!form.value.capabilityName) {
    toast.error('Capability is required')
    return
  }
  formSaving.value = true
  try {
    const input: CreateGrantInput = {
      capabilityName: form.value.capabilityName,
      contextKind: form.value.contextKind,
      contextRef: form.value.contextRef.trim(),
      pattern: form.value.pattern,
      mode: form.value.mode,
      limitCount: form.value.limitCount,
      limitWindowSeconds: form.value.limitWindowSeconds,
      reason: form.value.reason.trim(),
    }
    if (form.value.expiresInSeconds != null)
      input.expiresInSeconds = form.value.expiresInSeconds
    await createGrant(input)
    closeForm()
  }
  catch (e) {
    toast.error(errorMessage(e))
  }
  finally {
    formSaving.value = false
  }
}

// ── Revoke ───────────────────────────────────────────────────────────────────
const confirmRevokeId = ref<string | null>(null)
const revokingId = ref<string | null>(null)

async function handleRevoke(id: string) {
  revokingId.value = id
  try {
    await revokeGrant(id, capabilityFilter.value || undefined)
    confirmRevokeId.value = null
  }
  catch (e) {
    toast.error(errorMessage(e))
  }
  finally {
    revokingId.value = null
  }
}

// ── Display helpers ──────────────────────────────────────────────────────────
function formatDate(iso: string | null) {
  if (!iso)
    return '—'
  return new Date(iso).toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function formatContext(kind: string, ref: string): string {
  return ref ? `${kind}: ${ref}` : kind
}

function formatLimit(count: number, windowSeconds: number): string {
  return count > 0 ? `${count} / ${windowSeconds}s` : 'unlimited'
}

function isLegacy(grantedBy: string): boolean {
  return grantedBy === LEGACY_GRANTED_BY
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-start justify-between gap-3">
      <div>
        <h3 class="text-[17px] font-bold text-fg mb-1">
          Grants
        </h3>
        <p class="text-xs text-fg-mute">
          Capability grants control which agents may exercise a capability, where, and under what limits. Revoking keeps the grant visible as history.
        </p>
      </div>
      <AppButton variant="info" data-testid="grant-new" @click="openCreate">
        + New Grant
      </AppButton>
    </div>

    <div class="flex items-center gap-2">
      <label for="grant-filter" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Filter</label>
      <AppSelect
        id="grant-filter"
        :model-value="capabilityFilter"
        :options="capabilityFilterOptions"
        class="w-64"
        @update:model-value="onFilterChange"
      />
    </div>

    <div v-if="loading" class="text-center py-12 text-fg-mute text-sm">
      Loading grants...
    </div>
    <div v-else-if="!grants.length && !formVisible" class="text-center py-8 text-fg-mute text-sm">
      No grants yet. Create one to allow a capability in a given context.
    </div>

    <table v-else-if="!formVisible" class="w-full border-collapse text-[13px]">
      <thead>
        <tr>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Capability
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Context
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Pattern
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Mode
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Limit
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Expires
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Granted By
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Status
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Actions
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="g in grants" :key="g.id" :data-testid="`grant-row-${g.id}`" :class="{ 'opacity-60': g.revoked_at }">
          <td class="px-3 py-2.5 border-b border-line text-fg font-medium whitespace-nowrap">
            {{ g.capability_name }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
            {{ formatContext(g.context_kind, g.context_ref) }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
            {{ g.pattern || '*' }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute">
            {{ g.mode }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute whitespace-nowrap">
            {{ formatLimit(g.limit_count, g.limit_window_seconds) }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
            {{ formatDate(g.expires_at) }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute">
            <span v-if="isLegacy(g.granted_by)" class="inline-block rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide bg-raised text-fg-mute" title="Written by the legacy grant migration, not a person">
              Legacy migration
            </span>
            <span v-else>{{ g.granted_by }}</span>
          </td>
          <td class="px-3 py-2.5 border-b border-line">
            <span v-if="!g.revoked_at" class="inline-block rounded px-2 py-0.5 text-[11px] font-semibold bg-success-soft text-success-text">Active</span>
            <span v-else class="inline-block rounded px-2 py-0.5 text-[11px] font-semibold bg-raised text-fg-mute" :title="`Revoked ${formatDate(g.revoked_at)} by ${g.revoked_by}`">
              Revoked {{ formatDate(g.revoked_at) }} by {{ g.revoked_by }}
            </span>
          </td>
          <td class="px-3 py-2.5 border-b border-line whitespace-nowrap">
            <template v-if="!g.revoked_at">
              <template v-if="confirmRevokeId === g.id">
                <AppButton variant="danger" size="sm" class="mr-1" :disabled="revokingId === g.id" :data-testid="`grant-revoke-confirm-${g.id}`" @click="handleRevoke(g.id)">
                  {{ revokingId === g.id ? 'Revoking…' : 'Confirm' }}
                </AppButton>
                <AppButton variant="secondary" size="sm" @click="confirmRevokeId = null">
                  Cancel
                </AppButton>
              </template>
              <button
                v-else
                type="button"
                class="bg-transparent border-none text-fg-mute cursor-pointer text-sm px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400"
                :data-testid="`grant-revoke-${g.id}`"
                @click="confirmRevokeId = g.id"
              >
                Revoke
              </button>
            </template>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- Create form -->
    <div v-if="formVisible" class="flex flex-col gap-4">
      <div class="flex items-center justify-between">
        <h4 class="text-sm font-semibold text-fg">
          New Grant
        </h4>
        <button type="button" class="bg-transparent border-none text-fg-mute text-lg cursor-pointer px-1 leading-none hover:text-fg" @click="closeForm">
          &times;
        </button>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="grant-capability">Capability</label>
          <AppSelect
            id="grant-capability"
            v-model="form.capabilityName"
            data-testid="grant-capability"
            :options="capabilityOptions"
            class="w-full"
          />
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="grant-mode">Mode</label>
          <AppSelect
            id="grant-mode"
            v-model="form.mode"
            data-testid="grant-mode"
            :options="modeOptions"
            class="w-full"
          />
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="grant-context-kind">Context kind</label>
          <AppSelect
            id="grant-context-kind"
            :model-value="form.contextKind"
            data-testid="grant-context-kind"
            :options="contextKindOptions"
            class="w-full"
            @update:model-value="onContextKindChange"
          />
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="grant-context-ref">Context ref</label>
          <input
            id="grant-context-ref"
            v-model="form.contextRef"
            data-testid="grant-context-ref"
            type="text"
            :disabled="contextRefDisabled"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent disabled:opacity-50"
            :placeholder="contextRefDisabled ? 'not used for global' : 'e.g. project-123'"
          >
        </div>
        <div class="col-span-2">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="grant-pattern">Pattern</label>
          <input
            id="grant-pattern"
            v-model="form.pattern"
            data-testid="grant-pattern"
            type="text"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
            placeholder="* or a prefix pattern, e.g. git status*"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="grant-limit-count">Limit count (0 = unlimited)</label>
          <input
            id="grant-limit-count"
            v-model.number="form.limitCount"
            data-testid="grant-limit-count"
            type="number"
            min="0"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="grant-limit-window">Limit window (seconds)</label>
          <input
            id="grant-limit-window"
            v-model.number="form.limitWindowSeconds"
            data-testid="grant-limit-window"
            type="number"
            min="0"
            :disabled="form.limitCount <= 0"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent disabled:opacity-50"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="grant-expires">Expires in (seconds, optional)</label>
          <input
            id="grant-expires"
            v-model.number="form.expiresInSeconds"
            data-testid="grant-expires"
            type="number"
            min="1"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
            placeholder="never expires"
          >
        </div>
        <div class="col-span-2">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="grant-reason">Reason (optional)</label>
          <input
            id="grant-reason"
            v-model="form.reason"
            data-testid="grant-reason"
            type="text"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
            placeholder="Why this grant exists"
          >
        </div>
      </div>

      <div class="flex gap-2">
        <AppButton variant="info" data-testid="grant-submit" :disabled="formSaving" @click="handleCreate">
          {{ formSaving ? 'Creating…' : 'Create Grant' }}
        </AppButton>
        <AppButton variant="secondary" @click="closeForm">
          Cancel
        </AppButton>
      </div>
    </div>
  </div>
</template>
