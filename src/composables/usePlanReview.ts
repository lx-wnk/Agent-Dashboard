import type { PipelineTask } from '../types'
import { ref } from 'vue'
import { actionEndpoint } from './useRunAction'

export function usePlanReview(taskId: () => string | null) {
  const gateState = ref<string>('unknown')
  const approvedPlan = ref<Record<string, unknown> | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchStatus(): Promise<void> {
    const id = taskId()
    if (!id)
      return
    loading.value = true
    error.value = null
    try {
      const res = await fetch(`/api/plan/${id}/status`)
      if (!res.ok) {
        error.value = `Failed to load plan status: ${res.status}`
        return
      }
      const data = await res.json() as { gate_state: string, approved_plan?: Record<string, unknown> | null }
      gateState.value = data.gate_state
      approvedPlan.value = data.approved_plan ?? null
    }
    catch (err) {
      error.value = String(err)
    }
    finally {
      loading.value = false
    }
  }

  async function approve(): Promise<PipelineTask | null> {
    const id = taskId()
    if (!id)
      return null
    error.value = null
    try {
      const endpoint = actionEndpoint('approve_plan', id)!
      const res = await fetch(endpoint, { method: 'POST' })
      if (!res.ok) {
        const body = await res.json().catch(() => null)
        error.value = (body as { error?: string } | null)?.error ?? `Approve failed: ${res.status}`
        return null
      }
      return await res.json() as PipelineTask
    }
    catch (err) {
      error.value = String(err)
      return null
    }
  }

  async function reject(feedback: string): Promise<void> {
    const id = taskId()
    if (!id)
      return
    error.value = null
    try {
      const endpoint = actionEndpoint('reject_plan', id)!
      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ feedback }),
      })
      if (!res.ok) {
        const body = await res.json().catch(() => null)
        error.value = (body as { error?: string } | null)?.error ?? `Reject failed: ${res.status}`
      }
    }
    catch (err) {
      error.value = String(err)
    }
  }

  return {
    gateState,
    approvedPlan,
    loading,
    error,
    fetchStatus,
    approve,
    reject,
  }
}
