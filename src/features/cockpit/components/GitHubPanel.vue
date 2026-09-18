<script setup lang="ts">
import type { GitHubChecks } from '../composables/useGitHubSummary'
import type { PanelState } from '../panelState'
import { computed, onMounted } from 'vue'
import { useGitHubSummary } from '../composables/useGitHubSummary'
import CockpitPanel from './CockpitPanel.vue'

const { repos, loading, error, denied, unconfigured, fetchSummary } = useGitHubSummary()
onMounted(() => void fetchSummary())

const pullRequests = computed(() => repos.value.flatMap(r => r.pullRequests.map(pr => ({ ...pr, repo: r.repo }))))

// The route answers 200 with what it could reach and names what it could not,
// so one rate-limited repository does not blank the others. A repository that
// failed contributes no pull requests, so without this list it is
// indistinguishable from one that simply has none — a failure drawn as an
// empty answer, the exact defect panelState.ts exists to prevent.
const repoFailures = computed(() =>
  repos.value.filter(r => r.error).map(r => `${r.repo}: ${r.error}`),
)

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
  if (pullRequests.value.length > 0)
    return 'ready'
  // Nothing came back AND something broke: that is a failure, not an empty
  // answer. Only a clean run with no pull requests is 'empty'.
  return repoFailures.value.length > 0 ? 'failed' : 'empty'
})

// Shape and text carry the state, never hue alone: a red dot and a green dot
// read identically to a colour-blind eye and to a screen reader.
const CHECK_MARKS: Record<GitHubChecks['state'], string> = {
  success: 'OK',
  failure: 'X',
  pending: '...',
  none: '-',
}

function checkLabel(checks: GitHubChecks): string {
  switch (checks.state) {
    case 'success':
      return `${checks.passed}/${checks.total}`
    case 'failure':
      return `${checks.failed} failed`
    case 'pending':
      return `${checks.passed + checks.failed}/${checks.total}`
    case 'none':
      return 'no checks'
  }
}

// "none" means the lookup found nothing OR failed -- the handler collapses
// both, so the wording must not promise which one it was.
function checkTitle(checks: GitHubChecks): string {
  return checks.state === 'none'
    ? 'No check runs reported for this pull request'
    : `Checks ${checks.state}: ${checks.passed} passed, ${checks.failed} failed, ${checks.total} total`
}

const message = computed(() => {
  if (unconfigured.value)
    return 'Set github.token and github.repos in Settings → GitHub to switch this on.'
  if (denied.value)
    return denied.value
  return error.value
    ?? (repoFailures.value.length > 0 ? repoFailures.value.join('; ') : undefined)
    ?? 'No open pull request in the configured repositories.'
})
</script>

<template>
  <CockpitPanel id="github" title="GitHub" :state="state" :message="message">
    <p
      v-if="repoFailures.length > 0"
      data-testid="cockpit-github-partial-failure"
      class="text-[12px] rounded-md px-3 py-2 mb-2 bg-warning-soft text-warning-text"
      role="alert"
    >
      {{ repoFailures.join('; ') }}
    </p>
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
        <span
          v-if="pr.checks"
          :data-testid="`cockpit-github-checks-${pr.number}`"
          class="shrink-0 tabular-nums text-fg-mute"
          :title="checkTitle(pr.checks)"
          :aria-label="checkTitle(pr.checks)"
        >{{ CHECK_MARKS[pr.checks.state] }} {{ checkLabel(pr.checks) }}</span>
        <span class="shrink-0 text-fg-mute">{{ pr.repo }}#{{ pr.number }}</span>
      </li>
    </ul>
  </CockpitPanel>
</template>
