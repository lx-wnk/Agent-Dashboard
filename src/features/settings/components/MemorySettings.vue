<script setup lang="ts">
import type { SelectOption } from '@/components/ui/selectOption'
import type { CreateEntryInput, MemoryEntryKind, MemorySourceKind } from '@/features/settings/composables/useMemory'
import type { ResourceScopeKind } from '@/features/settings/composables/useResources'
import { computed, nextTick, ref } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import { MEMORY_ENTRY_KINDS, MEMORY_SOURCE_KINDS, MemoryWriteDeniedError, useMemory } from '@/features/settings/composables/useMemory'
import { RESOURCE_SCOPE_KINDS } from '@/features/settings/composables/useResources'
import { errorMessage } from '@/utils/errorMessage'

const {
  spaces,
  globalSpaces,
  entries,
  scope,
  searchText,
  loading,
  error,
  searchError,
  denied,
  searchDenied,
  searching,
  searched,
  entriesView,
  browsedSpaceId,
  held,
  fetchSpaces,
  searchEntries,
  browseSpace,
  leaveBrowse,
  setScope,
  createSpace,
  createEntry,
  supersedeEntry,
  expireEntry,
} = useMemory()

const ENTRY_KIND_OPTIONS: SelectOption<MemoryEntryKind>[] = MEMORY_ENTRY_KINDS.map(k => ({ value: k, label: k }))
const SOURCE_KIND_OPTIONS: SelectOption<MemorySourceKind>[] = MEMORY_SOURCE_KINDS.map(s => ({ value: s, label: s }))

function selectScopeKind(scopeKind: ResourceScopeKind) {
  if (scopeKind !== scope.value.scopeKind)
    void setScope({ scopeKind, scopeRef: scopeKind === 'global' ? '' : scope.value.scopeRef })
}

function onScopeRefChange(event: Event) {
  void setScope({ scopeKind: scope.value.scopeKind, scopeRef: (event.target as HTMLInputElement).value })
}

function formatConfidence(value: number): string {
  return value.toFixed(2)
}

const panelRef = ref<HTMLElement | null>(null)

// Closing a form unmounts the control that holds focus, which drops focus to
// <body> and loses the keyboard user's place in the panel. The control that
// opened the form is where they were, so that is where focus goes back.
async function returnFocusTo(testid: string): Promise<void> {
  await nextTick()
  panelRef.value?.querySelector<HTMLElement>(`[data-testid="${testid}"]`)?.focus()
}

// A write reports its outcome next to the control that made it — visible only.
// Success additionally travels through this live region, which is the only
// channel a screen-reader user has for a form that just closed.
const announcement = ref('')

// The two "nothing is being asserted yet" states, held in one place so the
// live region can render unconditionally and change only its contents (an
// ARIA live region inserted together with its text does not announce —
// cf. NotificationSettings.vue and App.vue's update banner).
const spacesStatus = computed<{ testid: string, text: string } | null>(() => {
  if (loading.value)
    return { testid: 'memory-loading', text: 'Loading memory spaces...' }
  if (held.value)
    return { testid: 'memory-held', text: 'Enter a scope ref to search.' }
  return null
})

// Same four states for the hits: in flight, not asked yet, refused/failed
// (rendered by the notices above, so this stays silent), and confirmed empty.
// Without `searched` the last two are one identical blank list.
const entriesStatus = computed<{ testid: string, text: string } | null>(() => {
  if (searching.value)
    return { testid: 'memory-entries-searching', text: 'Searching entries...' }
  if (held.value)
    return { testid: 'memory-entries-held', text: 'Enter a scope ref to search entries.' }
  if (searchDenied.value || searchError.value)
    return null
  if (!searched.value)
    return { testid: 'memory-entries-unsearched', text: 'No search has been run yet.' }
  if (!entries.value.length) {
    return entriesView.value === 'browse'
      ? { testid: 'memory-entries-browse-empty', text: 'This space has no entries yet.' }
      : { testid: 'memory-entries-empty', text: 'No entries matched this search.' }
  }
  return null
})

// The spaces table only ever lists exact-scope rows (that is what the scope
// selector means) but a hit's space can be a global one the retriever
// unioned in (server/internal/memory/retrieve.go:165-192) — so label
// resolution alone falls back to the separately-fetched global list.
function spaceLabel(spaceId: string): string {
  const local = spaces.value.find(s => s.id === spaceId)
  if (local)
    return local.slug
  const global = globalSpaces.value.find(s => s.id === spaceId)
  return global ? global.slug : spaceId
}

function isOutsideScope(spaceId: string): boolean {
  return !spaces.value.some(s => s.id === spaceId) && globalSpaces.value.some(s => s.id === spaceId)
}

// Every write is gated on memory.write, which a user holding memory.read may
// not have — so the panel renders fully and refuses only at the write. That
// refusal is a configuration state with a known fix, like the read side's
// `denied`, but it is reported per action, next to the control that hit it:
// folding it into `denied` would blank a panel the read grant legitimately
// filled, and gating the controls on the read grant would hide the write from
// a user who does hold it.
//
// The server's own message leads and this trails it as a likely cause, never
// as the diagnosis: handler.go's authorize() answers 403 for every
// Gate.Authorize failure — a rate limit and a failed grant-store read included.
const WRITE_DENIED_HINT = 'Most likely cause: memory.write is not granted here — open the Grants panel and add an allow grant for memory.write in this context.'

interface WriteFailure {
  message: string
  denied: boolean
}

function writeFailure(e: unknown, fallback: string): WriteFailure {
  return { message: errorMessage(e, fallback), denied: e instanceof MemoryWriteDeniedError }
}

function failureText(failure: WriteFailure): string {
  return failure.denied ? `${failure.message} — ${WRITE_DENIED_HINT}` : failure.message
}

const spaceFormVisible = ref(false)
const spaceForm = ref({ slug: '', name: '' })
const spaceSaving = ref(false)
const spaceFailure = ref<WriteFailure | null>(null)

async function handleCreateSpace() {
  spaceFailure.value = null
  announcement.value = ''
  const slug = spaceForm.value.slug.trim()
  if (!slug) {
    spaceFailure.value = { message: 'Slug is required.', denied: false }
    return
  }
  spaceSaving.value = true
  try {
    await createSpace({ slug, name: spaceForm.value.name.trim() })
    spaceFormVisible.value = false
    spaceForm.value = { slug: '', name: '' }
    announcement.value = `Memory space ${slug} created.`
    void returnFocusTo('memory-space-new')
  }
  catch (e) {
    spaceFailure.value = writeFailure(e, 'Failed to create space')
  }
  finally {
    spaceSaving.value = false
  }
}

function emptyEntryForm(): CreateEntryInput {
  return { spaceSlug: '', summary: '', content: '', kind: 'fact', sourceKind: 'user', sourceRef: '', confidence: 1 }
}

const entryFormVisible = ref(false)
const entryForm = ref<CreateEntryInput>(emptyEntryForm())
const entrySaving = ref(false)
const entryFailure = ref<WriteFailure | null>(null)

async function handleCreateEntry() {
  entryFailure.value = null
  announcement.value = ''
  // Validated and sent as one value: a padded slug that passes a trimmed
  // check but travels untrimmed misses capability.Match's exact comparison,
  // so the write 403s and the panel blames a grant the user already holds.
  const input: CreateEntryInput = {
    ...entryForm.value,
    spaceSlug: entryForm.value.spaceSlug.trim(),
    summary: entryForm.value.summary.trim(),
    content: entryForm.value.content.trim(),
  }
  if (!input.spaceSlug || !input.summary || !input.content) {
    entryFailure.value = { message: 'Space, summary and content are required.', denied: false }
    return
  }
  entrySaving.value = true
  try {
    await createEntry(input)
    entryFormVisible.value = false
    entryForm.value = emptyEntryForm()
    announcement.value = `Memory entry created in ${input.spaceSlug}.`
    void returnFocusTo('memory-entry-new')
  }
  catch (e) {
    entryFailure.value = writeFailure(e, 'Failed to create entry')
  }
  finally {
    entrySaving.value = false
  }
}

const supersedingId = ref<string | null>(null)
const supersededBy = ref('')
const confirmExpireId = ref<string | null>(null)
// Carries the entry id so the failure renders in the row it came from.
const entryActionFailure = ref<(WriteFailure & { id: string }) | null>(null)

function openSupersede(id: string) {
  supersedingId.value = id
  supersededBy.value = ''
}

function closeSupersede(id: string) {
  supersedingId.value = null
  void returnFocusTo(`memory-supersede-${id}`)
}

function closeExpire(id: string) {
  confirmExpireId.value = null
  void returnFocusTo(`memory-expire-${id}`)
}

async function handleSupersede(id: string) {
  entryActionFailure.value = null
  announcement.value = ''
  const replacement = supersededBy.value.trim()
  if (!replacement) {
    entryActionFailure.value = { id, message: 'The replacing entry id is required.', denied: false }
    return
  }
  try {
    await supersedeEntry(id, replacement)
    supersededBy.value = ''
    announcement.value = `Entry superseded by ${replacement}.`
    closeSupersede(id)
  }
  catch (e) {
    entryActionFailure.value = { id, ...writeFailure(e, 'Failed to supersede entry') }
  }
}

async function handleExpire(id: string) {
  entryActionFailure.value = null
  announcement.value = ''
  try {
    await expireEntry(id)
    announcement.value = 'Entry expired.'
    closeExpire(id)
  }
  catch (e) {
    entryActionFailure.value = { id, ...writeFailure(e, 'Failed to expire entry') }
  }
}
</script>

<template>
  <div ref="panelRef" class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Memory
      </h3>
      <p class="text-xs text-fg-mute">
        What the system has learned and kept: spaces group entries, entries hold one conclusion each with a confidence and a source. Reading requires the <code>memory.read</code> capability in the selected scope; creating, superseding and expiring require <code>memory.write</code> there.
      </p>
    </div>

    <!-- Rendered unconditionally so a write's outcome is announced when the
         text changes; a live region inserted together with its content is not. -->
    <div data-testid="memory-announcement" role="status" aria-live="polite" aria-atomic="true" class="sr-only">
      {{ announcement }}
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <span class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Scope</span>
      <button
        v-for="s in RESOURCE_SCOPE_KINDS"
        :key="s"
        type="button"
        :data-testid="`memory-scope-${s}`"
        class="px-2 py-0.5 rounded text-[11px] border border-line cursor-pointer"
        :class="s === scope.scopeKind ? 'bg-info-soft text-info-text border-transparent font-semibold' : 'bg-transparent text-fg-mute hover:text-fg'"
        :aria-pressed="s === scope.scopeKind"
        @click="selectScopeKind(s)"
      >
        {{ s }}
      </button>
      <template v-if="scope.scopeKind !== 'global'">
        <label for="memory-scope-ref" class="sr-only">Scope ref</label>
        <input
          id="memory-scope-ref"
          :value="scope.scopeRef"
          data-testid="memory-scope-ref"
          type="text"
          placeholder="scope ref, e.g. /home/me/project"
          class="w-72 bg-card border border-line rounded px-2.5 py-1 text-xs text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          @change="onScopeRefChange"
        >
      </template>
    </div>

    <div class="flex items-center justify-between">
      <h4 class="text-sm font-semibold text-fg">
        Spaces
      </h4>
      <AppButton
        size="sm"
        data-testid="memory-space-new"
        :aria-expanded="spaceFormVisible"
        aria-controls="memory-space-form"
        @click="spaceFormVisible = !spaceFormVisible"
      >
        + New space
      </AppButton>
    </div>
    <div v-if="spaceFormVisible" id="memory-space-form" class="grid grid-cols-2 gap-3">
      <div class="flex flex-col gap-1">
        <label for="memory-space-slug" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Slug</label>
        <input
          id="memory-space-slug"
          v-model="spaceForm.slug"
          data-testid="memory-space-slug"
          type="text"
          placeholder="slug, e.g. project-notes"
          class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
      </div>
      <div class="flex flex-col gap-1">
        <label for="memory-space-name" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Display name</label>
        <input
          id="memory-space-name"
          v-model="spaceForm.name"
          data-testid="memory-space-name"
          type="text"
          placeholder="display name"
          class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
      </div>
      <AppButton variant="info" data-testid="memory-space-submit" :disabled="spaceSaving" class="col-span-2 justify-self-start" @click="handleCreateSpace">
        {{ spaceSaving ? 'Creating…' : 'Create space' }}
      </AppButton>
    </div>
    <div v-if="spaceFailure" data-testid="memory-space-error" role="alert" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      {{ failureText(spaceFailure) }}
    </div>

    <div
      data-testid="memory-status"
      role="status"
      aria-live="polite"
      aria-atomic="true"
      class="text-center text-fg-mute text-sm"
      :class="spacesStatus ? 'py-8' : 'sr-only'"
    >
      <span v-if="spacesStatus" :data-testid="spacesStatus.testid">{{ spacesStatus.text }}</span>
    </div>

    <div v-if="denied" data-testid="memory-denied" role="alert" class="rounded border border-warning-line bg-warning-soft text-warning-text px-3 py-2 text-xs">
      <strong>{{ denied }}</strong>
      <span class="block mt-0.5">
        Memory spaces are hidden for this scope. Most likely cause: <code>memory.read</code> is not granted here — open the Grants panel and add an <code>allow</code> grant for <code>memory.read</code> in this context. A rate limit, an unanswered ask, or a failed read of the grant store answers 403 the same way.
      </span>
    </div>
    <div v-else-if="error" data-testid="memory-error" role="alert" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      <span>{{ error }}</span>
      <AppButton size="sm" variant="outline" data-testid="memory-retry" class="ml-2" @click="fetchSpaces()">
        Retry
      </AppButton>
    </div>
    <template v-else-if="!spacesStatus">
      <div v-if="!spaces.length" data-testid="memory-empty" class="text-center py-8 text-fg-mute text-sm">
        No memory spaces in this scope yet.
      </div>
      <table v-else class="w-full border-collapse text-[13px]">
        <thead>
          <tr>
            <th class="text-left text-[10px] uppercase tracking-wide text-fg-mute px-3 py-2 border-b border-line">
              Space
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
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in spaces" :key="s.id" :data-testid="`memory-space-${s.id}`">
            <td class="px-3 py-2.5 border-b border-line text-fg font-mono text-xs">
              <button
                type="button"
                :data-testid="`memory-space-browse-${s.id}`"
                class="cursor-pointer underline decoration-dotted underline-offset-2 hover:text-info-text focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent rounded"
                @click="browseSpace(s.id)"
              >
                {{ s.slug }}
              </button>
            </td>
            <td class="px-3 py-2.5 border-b border-line text-fg">
              {{ s.name || '—' }}
            </td>
            <td class="px-3 py-2.5 border-b border-line text-fg-mute font-mono text-xs">
              {{ s.scopeRef ? `${s.scopeKind}: ${s.scopeRef}` : s.scopeKind }}
            </td>
            <td class="px-3 py-2.5 border-b border-line text-fg-mute">
              {{ s.state }}
            </td>
          </tr>
        </tbody>
      </table>
    </template>

    <div class="flex items-center justify-between pt-2">
      <h4 class="text-sm font-semibold text-fg">
        Entries
      </h4>
      <AppButton
        size="sm"
        data-testid="memory-entry-new"
        :aria-expanded="entryFormVisible"
        aria-controls="memory-entry-form"
        @click="entryFormVisible = !entryFormVisible"
      >
        + New entry
      </AppButton>
    </div>
    <div v-if="entryFormVisible" id="memory-entry-form" class="grid grid-cols-2 gap-3">
      <div class="flex flex-col gap-1">
        <label for="memory-entry-space" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Space slug</label>
        <input
          id="memory-entry-space"
          v-model="entryForm.spaceSlug"
          data-testid="memory-entry-space"
          type="text"
          placeholder="space slug"
          list="memory-entry-space-options"
          class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg font-mono focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
        <!-- Suggestions only, never a constraint: createEntry deliberately
             never creates a space on the fly, but memory.read and
             memory.write are separate grants — a scope with a denied or
             empty spaces table must still accept a typed slug. -->
        <datalist id="memory-entry-space-options" data-testid="memory-entry-space-options">
          <option v-for="s in spaces" :key="s.id" :value="s.slug" />
        </datalist>
      </div>
      <div class="flex flex-col gap-1">
        <label for="memory-entry-summary" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Summary</label>
        <input
          id="memory-entry-summary"
          v-model="entryForm.summary"
          data-testid="memory-entry-summary"
          type="text"
          placeholder="summary — this is what gets pushed into a spawn"
          class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
      </div>
      <div class="col-span-2 flex flex-col gap-1">
        <label for="memory-entry-content" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Content</label>
        <textarea
          id="memory-entry-content"
          v-model="entryForm.content"
          data-testid="memory-entry-content"
          rows="4"
          placeholder="content — pulled on demand"
          class="bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        />
      </div>
      <div class="flex flex-col gap-1">
        <label for="memory-entry-kind" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Kind</label>
        <AppSelect
          id="memory-entry-kind"
          v-model="entryForm.kind"
          :options="ENTRY_KIND_OPTIONS"
          data-testid="memory-entry-kind"
          class="w-full"
        />
      </div>
      <div class="flex flex-col gap-1">
        <label for="memory-entry-source-kind" class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Source kind</label>
        <AppSelect
          id="memory-entry-source-kind"
          v-model="entryForm.sourceKind"
          :options="SOURCE_KIND_OPTIONS"
          data-testid="memory-entry-source-kind"
          class="w-full"
        />
      </div>
      <div v-if="entryFailure" data-testid="memory-entry-error" role="alert" class="col-span-2 rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
        {{ failureText(entryFailure) }}
      </div>
      <AppButton variant="info" data-testid="memory-entry-submit" :disabled="entrySaving" class="col-span-2 justify-self-start" @click="handleCreateEntry">
        {{ entrySaving ? 'Creating…' : 'Create entry' }}
      </AppButton>
    </div>

    <!-- Sibling of the state chain above, not nested inside it: the spaces
         list can be empty for this exact scope while entries are still
         searchable (the retriever also unions in every global space), so
         search must stay reachable regardless of what the spaces table shows. -->
    <div class="flex items-center gap-2 pt-2 border-t border-line">
      <label for="memory-search-input" class="sr-only">Search entries in this scope</label>
      <input
        id="memory-search-input"
        v-model="searchText"
        data-testid="memory-search-input"
        type="text"
        placeholder="Search entries in this scope"
        class="flex-1 bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        @keyup.enter="searchEntries()"
      >
      <AppButton data-testid="memory-search-submit" @click="searchEntries()">
        Search
      </AppButton>
    </div>

    <!-- Browsing replaces the search's results with one space's entries; the
         banner names which space so leaving it (back to whatever the search
         box still holds) is not a guess. -->
    <div v-if="entriesView === 'browse'" data-testid="memory-browse-banner" class="flex items-center gap-2 text-xs text-fg-mute">
      <span>Browsing <code>{{ spaceLabel(browsedSpaceId ?? '') }}</code></span>
      <AppButton size="sm" variant="ghost" data-testid="memory-browse-leave" @click="leaveBrowse()">
        Back to search
      </AppButton>
    </div>

    <!-- The search's own denial, rendered inline: the spaces table above may
         have loaded perfectly under the same grant check, and replacing the
         panel with this would throw that away. -->
    <div v-if="searchDenied" data-testid="memory-search-denied" role="alert" class="rounded border border-warning-line bg-warning-soft text-warning-text px-3 py-2 text-xs">
      <strong>{{ searchDenied }}</strong>
      <span class="block mt-0.5">
        No entries are listed for this search. Most likely cause: <code>memory.read</code> is not granted here — open the Grants panel and add an <code>allow</code> grant for <code>memory.read</code> in this context. A rate limit, an unanswered ask, or a failed read of the grant store answers 403 the same way.
      </span>
    </div>
    <div v-else-if="searchError" data-testid="memory-search-error" role="alert" class="rounded border border-danger-line bg-danger-soft text-danger-text px-3 py-2 text-xs">
      {{ searchError }}
    </div>

    <div
      data-testid="memory-entries-status"
      role="status"
      aria-live="polite"
      aria-atomic="true"
      class="text-center text-fg-mute text-sm"
      :class="entriesStatus ? 'py-6' : 'sr-only'"
    >
      <span v-if="entriesStatus" :data-testid="entriesStatus.testid">{{ entriesStatus.text }}</span>
    </div>

    <div v-for="e in entries" :key="e.id" :data-testid="`memory-entry-${e.id}`" class="px-3 py-2.5 bg-app rounded-md">
      <div class="flex items-center gap-2.5 mb-1">
        <span class="font-semibold text-xs text-fg">{{ e.summary }}</span>
        <span class="ml-auto font-mono text-[11px] text-fg-mute">{{ e.kind }} · {{ formatConfidence(e.confidence) }}</span>
      </div>
      <div class="text-[11px] text-fg-mute">
        in <code>{{ spaceLabel(e.spaceId) }}</code>
        <span v-if="isOutsideScope(e.spaceId)" :data-testid="`memory-entry-outside-scope-${e.id}`" class="italic">
          — global, outside this scope
        </span>
      </div>
      <p class="text-[11px] text-fg-mute mt-1 whitespace-pre-wrap">
        {{ e.content }}
      </p>
      <div class="flex items-center gap-2 mt-2">
        <template v-if="supersedingId === e.id">
          <label :for="`memory-supersede-input-${e.id}`" class="sr-only">Id of the entry replacing {{ e.summary }}</label>
          <input
            :id="`memory-supersede-input-${e.id}`"
            v-model="supersededBy"
            :data-testid="`memory-supersede-input-${e.id}`"
            type="text"
            placeholder="id of the replacing entry"
            class="w-64 bg-card border border-line rounded px-2 py-1 text-xs text-fg font-mono"
          >
          <AppButton size="sm" variant="info" :data-testid="`memory-supersede-confirm-${e.id}`" @click="handleSupersede(e.id)">
            Confirm
          </AppButton>
          <AppButton size="sm" variant="ghost" @click="closeSupersede(e.id)">
            Cancel
          </AppButton>
        </template>
        <AppButton v-else size="sm" variant="ghost" :data-testid="`memory-supersede-${e.id}`" @click="openSupersede(e.id)">
          Supersede
        </AppButton>

        <template v-if="confirmExpireId === e.id">
          <AppButton size="sm" variant="danger" :data-testid="`memory-expire-confirm-${e.id}`" @click="handleExpire(e.id)">
            Confirm expire
          </AppButton>
          <AppButton size="sm" variant="ghost" @click="closeExpire(e.id)">
            Cancel
          </AppButton>
        </template>
        <AppButton v-else size="sm" variant="ghost" :data-testid="`memory-expire-${e.id}`" @click="confirmExpireId = e.id">
          Expire
        </AppButton>
      </div>
      <p v-if="entryActionFailure?.id === e.id" :data-testid="`memory-entry-action-error-${e.id}`" role="alert" class="text-[11px] text-danger-text mt-1">
        {{ failureText(entryActionFailure) }}
      </p>
    </div>
  </div>
</template>
