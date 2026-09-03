import { ref } from 'vue'
import { errorMessage, readErrorMessage } from '@/utils/errorMessage'

// Mirrors pullRequestView / repoSummary in server/internal/api/github/handler.go.
// That handler owns the wire shape; these names must match its json tags.
export interface GitHubPullRequest {
  number: number
  title: string
  author: string
  url: string
  draft: boolean
  updatedAt: string
}

export interface GitHubRepoSummary {
  repo: string
  pullRequests: GitHubPullRequest[]
  error?: string
}

// States only what is known. The route maps every Gate.Authorize failure to
// 403 — missing grant, rate limit, unreadable grant store alike — so the
// fallback names no cause, matching useResources' DENIED_FALLBACK.
const DENIED_FALLBACK = 'The GitHub route refused this read (HTTP 403) without giving a reason.'

export function useGitHubSummary() {
  const repos = ref<GitHubRepoSummary[]>([])
  const loading = ref(true)
  const error = ref<string | null>(null)
  const denied = ref<string | null>(null)
  // 503 from the route means github.token/github.repos are unset: the request
  // was answered, but nothing was ever asked of GitHub. Held apart from
  // `error` because the fix is different — configure it, not repair it.
  const unconfigured = ref(false)

  async function fetchSummary(): Promise<void> {
    loading.value = true
    error.value = null
    denied.value = null
    unconfigured.value = false
    try {
      const res = await fetch('/api/github/summary')
      if (res.status === 503) {
        unconfigured.value = true
        repos.value = []
        return
      }
      if (res.status === 403) {
        denied.value = await readErrorMessage(res, DENIED_FALLBACK)
        repos.value = []
        return
      }
      if (!res.ok)
        throw new Error(await readErrorMessage(res, `Failed to load the GitHub summary (HTTP ${res.status})`))
      const body = await res.json() as { repos?: GitHubRepoSummary[] }
      repos.value = body.repos ?? []
    }
    catch (e) {
      // Cleared on failure: leaving the previous answer on screen under a
      // failure notice would misreport what GitHub holds now.
      repos.value = []
      error.value = errorMessage(e, 'Failed to load the GitHub summary')
    }
    finally {
      loading.value = false
    }
  }

  return { repos, loading, error, denied, unconfigured, fetchSummary }
}
