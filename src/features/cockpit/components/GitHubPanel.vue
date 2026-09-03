<script setup lang="ts">
import type { PanelState } from '../panelState'
import { computed, onMounted } from 'vue'
import { useGitHubSummary } from '../composables/useGitHubSummary'
import CockpitPanel from './CockpitPanel.vue'

const { repos, loading, error, denied, unconfigured, fetchSummary } = useGitHubSummary()
onMounted(() => void fetchSummary())

const pullRequests = computed(() => repos.value.flatMap(r => r.pullRequests.map(pr => ({ ...pr, repo: r.repo }))))

// The order matters and is the whole five-state rule: a refusal is checked
// before an empty answer, so a denied read can never be drawn as "no open
// pull requests", and "not configured" is checked before both, so an
// unconfigured install never looks like a healthy quiet one.
const state = computed<PanelState>(() => {
  if (loading.value)
    return 'loading'
  if (unconfigured.value)
    return 'notAsked'
  if (denied.value)
    return 'denied'
  if (error.value)
    return 'failed'
  return pullRequests.value.length === 0 ? 'empty' : 'ready'
})

const message = computed(() => {
  if (unconfigured.value)
    return 'Set github.token and github.repos in Settings → GitHub to switch this on.'
  return denied.value ?? error.value ?? 'No open pull request in the configured repositories.'
})
</script>

<template>
  <CockpitPanel id="github" title="GitHub" :state="state" :message="message">
    <ul class="flex flex-col gap-1.5">
      <li
        v-for="pr in pullRequests.slice(0, 8)"
        :key="`${pr.repo}#${pr.number}`"
        class="flex items-center justify-between gap-2 text-[12px] min-w-0"
        :data-testid="`cockpit-github-pr-${pr.number}`"
      >
        <a :href="pr.url" target="_blank" rel="noopener noreferrer" class="truncate text-fg hover:text-accent">
          {{ pr.title }}
        </a>
        <span class="shrink-0 text-fg-mute">{{ pr.repo }}#{{ pr.number }}</span>
      </li>
    </ul>
  </CockpitPanel>
</template>
