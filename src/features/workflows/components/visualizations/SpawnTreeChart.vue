<script setup lang="ts">
import type { SpawnTreeData } from '@/sdk.generated'
import { computed, ref, watch } from 'vue'
import { useTheme } from '@/composables/useTheme'
import { toast } from '@/composables/useToast'
import { paletteColor } from '@/utils/chartColors'

const props = defineProps<{
  data: SpawnTreeData | null
  loading: boolean
  error: string | null
}>()

const emit = defineEmits<{ navigate: [sessionId: string] }>()

const isEmpty = computed(() => !props.data || props.data.nodes.length === 0)

// Ordinal color from design-system palette for model dots.
const { theme } = useTheme()

// Surface data-fetch errors (from the parent view) as toasts.
watch(() => props.error, (msg) => {
  if (msg)
    toast.error(msg)
}, { immediate: true })

interface SessionRow {
  id: string
  label: string
  toolCount: number
  costCents: number
  model: string
  subagentCount: number
}

interface ProjectGroup {
  project: string
  sessions: SessionRow[]
}

const modelColorCache = new Map<string, string>()
let modelColorIndex = 0

function colorForModel(model: string): string {
  if (!model)
    return 'currentColor'
  if (modelColorCache.has(model))
    return modelColorCache.get(model)!
  const color = paletteColor(modelColorIndex)
  modelColorIndex++
  modelColorCache.set(model, color)
  return color
}

function formatCost(costCents: number): string {
  return `$${(costCents / 100).toFixed(2)}`
}

const groups = computed<ProjectGroup[]>(() => {
  void theme.value // track theme so colors refresh on toggle
  modelColorCache.clear()
  modelColorIndex = 0

  if (!props.data || props.data.nodes.length === 0)
    return []

  const { roots, nodes, links } = props.data

  // Build children map: parent id → child ids.
  const childrenOf = new Map<string, string[]>()
  for (const link of links) {
    if (!childrenOf.has(link.source))
      childrenOf.set(link.source, [])
    childrenOf.get(link.source)!.push(link.target)
  }

  // Count total descendants (subagents) via BFS for each root.
  function countDescendants(id: string): number {
    let count = 0
    const queue = [...(childrenOf.get(id) ?? [])]
    while (queue.length > 0) {
      const next = queue.shift()!
      count++
      const kids = childrenOf.get(next)
      if (kids) {
        queue.push(...kids)
      }
    }
    return count
  }

  const byId = new Map(nodes.map(n => [n.id, n]))
  const rootSet = new Set(roots)

  // Build per-project groups from root sessions only.
  const projectMap = new Map<string, SessionRow[]>()
  for (const rootId of roots) {
    const node = byId.get(rootId)
    if (!node)
      continue

    const projectKey = node.project?.trim() || '(unknown)'

    // If the label equals the project name (unhelpful), fall back to id prefix.
    const rawLabel = node.label?.trim()
    const displayLabel = rawLabel && rawLabel !== node.project
      ? rawLabel
      : rootId.slice(0, 8)

    const row: SessionRow = {
      id: rootId,
      label: displayLabel,
      toolCount: node.toolCount,
      costCents: node.costCents,
      model: node.model ?? '',
      subagentCount: countDescendants(rootId),
    }

    if (!projectMap.has(projectKey))
      projectMap.set(projectKey, [])
    projectMap.get(projectKey)!.push(row)
  }

  // Also include any root nodes that appear in nodes but not in roots array,
  // in case backends differ — skip non-root nodes (subagents).
  // (No additional handling needed; we only show root sessions by design.)
  void rootSet // used above implicitly through the roots array

  // Sort sessions within each project by toolCount desc.
  for (const sessions of projectMap.values()) {
    sessions.sort((a, b) => b.toolCount - a.toolCount)
  }

  // Sort groups by project name, "(unknown)" last.
  const sorted = [...projectMap.entries()]
    .sort(([a], [b]) => {
      if (a === '(unknown)')
        return 1
      if (b === '(unknown)')
        return -1
      return a.localeCompare(b)
    })
    .map(([project, sessions]) => ({ project, sessions }))

  // Pre-register colors in a deterministic order so they're stable across re-renders.
  for (const { sessions } of sorted) {
    for (const s of sessions) {
      colorForModel(s.model)
    }
  }

  return sorted
})

// Tracks which project groups are expanded (by project name).
// Empty by default → all groups start collapsed.
const expanded = ref(new Set<string>())

function toggleProject(project: string) {
  const next = new Set(expanded.value)
  if (next.has(project)) {
    next.delete(project)
  }
  else {
    next.add(project)
  }
  expanded.value = next
}

function isExpanded(project: string): boolean {
  return expanded.value.has(project)
}

function onSessionClick(id: string) {
  emit('navigate', id)
}

function onSessionKeydown(event: KeyboardEvent, id: string) {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    emit('navigate', id)
  }
}
</script>

<template>
  <div class="spawn-tree-chart">
    <div v-if="loading" class="text-sm text-fg-mute p-4">
      Loading spawn tree…
    </div>
    <div v-else-if="isEmpty" class="text-sm text-fg-mute p-4">
      No sessions found in this window.
    </div>
    <div v-else class="py-2">
      <div
        v-for="group in groups"
        :key="group.project"
        class="mb-1"
      >
        <!-- Project header row -->
        <button
          type="button"
          class="w-full flex items-center gap-1.5 px-3 py-1.5 text-left hover:bg-raised rounded transition-colors focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:outline-none"
          :aria-expanded="isExpanded(group.project)"
          @click="toggleProject(group.project)"
        >
          <span class="text-fg-mute text-[11px] leading-none select-none">
            {{ isExpanded(group.project) ? '▾' : '▸' }}
          </span>
          <span class="text-xs font-semibold text-fg">{{ group.project }}</span>
          <span class="text-[11px] text-fg-mute ml-1">
            {{ group.sessions.length }} session{{ group.sessions.length === 1 ? '' : 's' }}
          </span>
        </button>

        <!-- Session rows (visible when expanded) -->
        <div v-if="isExpanded(group.project)">
          <button
            v-for="session in group.sessions"
            :key="session.id"
            type="button"
            class="w-full flex items-start gap-2 pl-6 pr-3 py-1.5 text-left hover:bg-raised rounded transition-colors focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:outline-none cursor-pointer"
            @click="onSessionClick(session.id)"
            @keydown="onSessionKeydown($event, session.id)"
          >
            <!-- Model color dot -->
            <span
              class="mt-[3px] flex-none w-2 h-2 rounded-full"
              :style="{ backgroundColor: colorForModel(session.model) }"
              :title="session.model || 'unknown model'"
              aria-hidden="true"
            />

            <div class="min-w-0 flex-1">
              <!-- Label -->
              <div class="text-xs text-fg truncate leading-snug">
                {{ session.label }}
              </div>
              <!-- Metadata line -->
              <div class="flex items-center gap-2 mt-0.5 text-[11px] text-fg-mute flex-wrap">
                <span v-if="session.toolCount > 0">{{ session.toolCount }} tools</span>
                <span v-if="session.subagentCount > 0">+{{ session.subagentCount }} sub</span>
                <span v-if="session.costCents > 0">{{ formatCost(session.costCents) }}</span>
              </div>
            </div>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
