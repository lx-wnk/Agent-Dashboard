<script setup lang="ts">
import type { ResourceKind, ResourceScopeKind } from '@/features/settings/composables/useResources'
import {
  RESOURCE_KIND_LABELS,
  RESOURCE_KINDS,
  RESOURCE_SCOPE_KINDS,
  useResources,
} from '@/features/settings/composables/useResources'

const { resources, query, loading, error, held, fetchResources } = useResources()

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

function onScopeRefInput(event: Event) {
  query.value.scopeRef = (event.target as HTMLInputElement).value
}

const EMPTY_MESSAGES: Record<ResourceKind, string> = {
  application: 'No applications registered yet.',
  routine: 'No routines registered yet. Nothing writes routines to the registry today.',
  skill: 'No skills registered yet. Nothing writes skills to the registry today.',
  memory_space: 'No memory spaces registered yet. Create one from the Memory panel.',
}

function formatScope(scopeKind: string, scopeRef: string): string {
  return scopeRef ? `${scopeKind}: ${scopeRef}` : scopeKind
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
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
        @click="selectScopeKind(s)"
      >
        {{ s }}
      </button>
      <input
        v-if="query.scopeKind !== 'global'"
        :value="query.scopeRef"
        data-testid="resource-scope-ref"
        type="text"
        placeholder="scope ref, e.g. /home/me/project"
        class="w-72 bg-card border border-line rounded px-2.5 py-1 text-xs text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        @input="onScopeRefInput"
        @change="fetchResources()"
      >
    </div>

    <div v-if="loading" data-testid="resource-loading" class="text-center py-12 text-fg-mute text-sm">
      Loading registry...
    </div>
    <div v-else-if="held" data-testid="resource-held" class="text-center py-8 text-fg-mute text-sm">
      Enter a scope ref to search.
    </div>
    <div v-else-if="error" data-testid="resource-error" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      {{ error }}
    </div>
    <div v-else-if="!resources.length" data-testid="resource-empty" class="text-center py-8 text-fg-mute text-sm">
      {{ EMPTY_MESSAGES[query.kind] }}
    </div>

    <table v-else class="w-full border-collapse text-[13px]">
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
            {{ formatDate(r.updatedAt) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
