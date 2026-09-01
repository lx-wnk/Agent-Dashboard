<script setup lang="ts">
import type { ResourceKind, ResourceScopeKind } from '@/features/settings/composables/useResources'
import { computed } from 'vue'
import {
  RESOURCE_KIND_LABELS,
  RESOURCE_KINDS,
  RESOURCE_SCOPE_KINDS,
  useResources,
} from '@/features/settings/composables/useResources'
import { formatDateTime, formatScope } from '@/utils/format'

const { resources, query, loading, error, denied, held, fetchResources } = useResources()

function selectKind(kind: ResourceKind) {
  if (kind !== query.value.kind)
    void fetchResources({ kind })
}

// The server refuses a ref on `global` and requires one on every other kind,
// so clearing it here keeps that half of the rule unreachable from the form —
// the same handling GrantSettings applies to a grant's context ref.
function selectScopeKind(scopeKind: ResourceScopeKind) {
  if (scopeKind === query.value.scopeKind)
    return
  if (scopeKind === 'global')
    void fetchResources({ scopeKind, scopeRef: '' })
  else
    void fetchResources({ scopeKind })
}

// `change`, never `input`: the typed ref reaches `query` only by way of
// fetchResources, so "the user named a scope" and "we asked about that scope"
// are the same event. Writing it per keystroke let `held` go false while
// `resources` still held the previous query's rows, and the panel then called a
// scope it had never requested empty.
function onScopeRefChange(event: Event) {
  void fetchResources({ scopeRef: (event.target as HTMLInputElement).value })
}

const EMPTY_MESSAGES: Record<ResourceKind, string> = {
  application: 'No applications registered yet.',
  routine: 'No routines registered yet. Nothing writes routines to the registry today.',
  skill: 'No skills registered yet. Nothing writes skills to the registry today.',
  memory_space: 'No memory spaces registered yet. Create one from the Memory panel.',
}

// One owner for "which of the five things is this panel saying". Each branch
// reads the composable's state; none of them re-derives it.
type PanelState = 'loading' | 'held' | 'denied' | 'error' | 'empty' | 'rows'

const panelState = computed<PanelState>(() => {
  if (loading.value)
    return 'loading'
  if (held.value)
    return 'held'
  if (denied.value)
    return 'denied'
  if (error.value)
    return 'error'
  return resources.value.length ? 'rows' : 'empty'
})

const announcing = computed(() => panelState.value === 'loading' || panelState.value === 'held')
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Registry
      </h3>
      <p class="text-xs text-fg-mute">
        Every managed resource the system knows about — applications, routines, skills and memory spaces — with its scope, lifecycle state and where it came from. Read-only: state changes belong to the subsystem that owns the resource.
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-1">
      <button
        v-for="k in RESOURCE_KINDS"
        :key="k"
        type="button"
        :data-testid="`resource-kind-${k}`"
        class="px-2.5 py-1 rounded text-xs border border-line cursor-pointer"
        :class="k === query.kind ? 'bg-accent-soft text-accent border-transparent font-semibold' : 'bg-transparent text-fg-mute hover:text-fg'"
        :aria-pressed="k === query.kind"
        @click="selectKind(k)"
      >
        {{ RESOURCE_KIND_LABELS[k] }}
      </button>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <span class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Scope</span>
      <button
        v-for="s in RESOURCE_SCOPE_KINDS"
        :key="s"
        type="button"
        :data-testid="`resource-scope-${s}`"
        class="px-2 py-0.5 rounded text-[11px] border border-line cursor-pointer"
        :class="s === query.scopeKind ? 'bg-info-soft text-info-text border-transparent font-semibold' : 'bg-transparent text-fg-mute hover:text-fg'"
        :aria-pressed="s === query.scopeKind"
        @click="selectScopeKind(s)"
      >
        {{ s }}
      </button>
      <template v-if="query.scopeKind !== 'global'">
        <label for="resource-scope-ref" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Scope ref</label>
        <input
          id="resource-scope-ref"
          :value="query.scopeRef"
          data-testid="resource-scope-ref"
          type="text"
          placeholder="scope ref, e.g. /home/me/project"
          class="w-72 bg-card border border-line rounded px-2.5 py-1 text-xs text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          @change="onScopeRefChange"
        >
      </template>
    </div>

    <!-- Mounted unconditionally, contents swapped: a live region inserted
         together with its text is not announced. -->
    <div
      role="status"
      aria-live="polite"
      aria-atomic="true"
      data-testid="resource-status"
      :class="announcing ? 'text-center py-8 text-fg-mute text-sm' : 'sr-only'"
    >
      <span v-if="panelState === 'loading'" data-testid="resource-loading">Loading registry...</span>
      <span v-else-if="panelState === 'held'" data-testid="resource-held">Enter a scope ref to search.</span>
    </div>

    <div v-if="panelState === 'denied'" data-testid="resource-denied" role="alert" class="rounded border border-warning-line bg-warning-soft text-warning-text px-3 py-2 text-xs">
      <strong>{{ denied }}</strong>
      <span class="block mt-0.5">
        Memory spaces are hidden for this scope. Most likely cause: <code>memory.read</code> is not granted here — open the Grants panel and add an <code>allow</code> grant for <code>memory.read</code> in this context. A rate limit, an unanswered ask, or a failed read of the grant store answers 403 the same way.
      </span>
    </div>
    <div v-else-if="panelState === 'error'" data-testid="resource-error" role="alert" class="flex items-center justify-between gap-3 rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      <span>{{ error }}</span>
      <button
        type="button"
        data-testid="resource-retry"
        class="shrink-0 rounded border border-danger-line px-2 py-0.5 text-xs font-semibold cursor-pointer hover:opacity-80"
        @click="fetchResources()"
      >
        Retry
      </button>
    </div>
    <div v-else-if="panelState === 'empty'" data-testid="resource-empty" class="text-center py-8 text-fg-mute text-sm">
      {{ EMPTY_MESSAGES[query.kind] }}
    </div>

    <table v-else-if="panelState === 'rows'" class="w-full border-collapse text-[13px]">
      <thead>
        <tr>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Slug
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Name
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Scope
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            State
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Version
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Origin
          </th>
          <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
            Updated
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in resources" :key="r.id" :data-testid="`resource-row-${r.id}`">
          <td class="px-3 py-2.5 border-b border-line text-fg font-mono text-xs">
            {{ r.slug }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg">
            {{ r.name || '—' }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
            {{ formatScope(r.scopeKind, r.scopeRef) }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute">
            {{ r.state }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
            {{ r.version || '—' }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute" :title="r.originRef">
            {{ r.origin }}
          </td>
          <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs whitespace-nowrap">
            {{ formatDateTime(r.updatedAt) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
