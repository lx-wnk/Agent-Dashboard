import type { WorktreeStatusDTO } from '../sdk.generated'
import { onUnmounted, ref, watch } from 'vue'
import type { Ref } from 'vue'

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
      error.value = (err as Error).message
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

  onUnmounted(stopPolling)

  return { status, isLoading, error, refresh }
}
