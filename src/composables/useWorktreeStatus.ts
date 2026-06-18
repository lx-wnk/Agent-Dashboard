import type { Ref } from 'vue'
import type { WorktreeStatusDTO } from '../sdk.generated'
import { onUnmounted, ref, watch } from 'vue'
import { errorMessage } from '../utils/errorMessage'

const POLL_MS = 30_000

/**
 * Fetches `/api/tasks/:id/worktree` for the active task. Returns a reactive
 * `status` ref (null = no worktree / not yet loaded), an `isLoading` flag,
 * and a `refresh()` helper.
 *
 * Polling is gated on `active`. While the modal is open, the composable
 * polls every 30 s; when `active` flips to false the timer is cleared. The
 * caller controls liveness — no SSE channel exists for worktree state.
 */
export function useWorktreeStatus(
  taskId: Ref<string | null>,
  active: Ref<boolean>,
) {
  const status = ref<WorktreeStatusDTO | null>(null)
  const isLoading = ref(false)
  const error = ref<string | null>(null)
  let timer: ReturnType<typeof setInterval> | null = null
  let activeFetchId: string | null = null

  async function fetchOnce(id: string): Promise<void> {
    activeFetchId = id
    isLoading.value = true
    error.value = null
    try {
      const res = await fetch(`/api/tasks/${id}/worktree`)
      // Race-guard: if the active id changed mid-flight, drop the result.
      if (activeFetchId !== id)
        return
      if (res.status === 204) {
        status.value = null
        return
      }
      if (!res.ok)
        throw new Error(`HTTP ${res.status}`)
      status.value = await res.json() as WorktreeStatusDTO
    }
    catch (err) {
      if (activeFetchId !== id)
        return
      error.value = errorMessage(err)
      status.value = null
    }
    finally {
      if (activeFetchId === id)
        isLoading.value = false
    }
  }

  async function refresh(): Promise<void> {
    if (taskId.value)
      await fetchOnce(taskId.value)
  }

  function stopPolling(): void {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
  }

  function startPolling(): void {
    stopPolling()
    if (!active.value || !taskId.value)
      return
    timer = setInterval(() => {
      if (taskId.value)
        void fetchOnce(taskId.value)
    }, POLL_MS)
  }

  watch(
    [taskId, active],
    ([id, isActive]) => {
      stopPolling()
      if (!isActive || !id) {
        status.value = null
        return
      }
      void fetchOnce(id)
      startPolling()
    },
    { immediate: true },
  )

  /**
   * POST /api/tasks/:id/worktree — creates the worktree. Refreshes status on success.
   */
  async function create(): Promise<void> {
    if (!taskId.value)
      return
    const id = taskId.value
    isLoading.value = true
    error.value = null
    try {
      const res = await fetch(`/api/tasks/${id}/worktree`, { method: 'POST' })
      if (!res.ok) {
        error.value = `Failed to create worktree (HTTP ${res.status})`
        return
      }
      await refresh()
    }
    catch (err) {
      error.value = (err as Error).message
    }
    finally {
      isLoading.value = false
    }
  }

  /**
   * DELETE /api/tasks/:id/worktree — removes the worktree.
   * Returns the HTTP status (204 = ok, 409 = dirty without force, 404 = not found, 0 = network error).
   * On 204 the status ref is refreshed (becomes null). On error sets error.value.
   */
  async function remove(force: boolean): Promise<number> {
    if (!taskId.value)
      return 0
    const id = taskId.value
    const url = `/api/tasks/${id}/worktree${force ? '?force=true' : ''}`
    error.value = null
    try {
      const res = await fetch(url, { method: 'DELETE' })
      if (res.status === 204) {
        await refresh()
        return 204
      }
      if (res.status === 409) {
        error.value = 'Worktree has uncommitted changes'
        return 409
      }
      error.value = `Failed to remove worktree (HTTP ${res.status})`
      return res.status
    }
    catch (err) {
      error.value = (err as Error).message
      return 0
    }
  }

  onUnmounted(stopPolling)

  return { status, isLoading, error, refresh, create, remove }
}
