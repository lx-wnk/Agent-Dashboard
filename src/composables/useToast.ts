import { ref } from 'vue'

export interface Toast {
  id: string
  message: string
  type: 'error' | 'success' | 'info'
}

const DEFAULT_DURATION_MS = 5000

// Module-level state — singleton across all importers.
const toasts = ref<Toast[]>([])

interface TimerEntry {
  remaining: number
  startedAt: number
  timerId: ReturnType<typeof setTimeout>
}
const timers = new Map<string, TimerEntry>()

function add(message: string, type: Toast['type'], duration = DEFAULT_DURATION_MS): string {
  const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`
  toasts.value.push({ id, message, type })
  schedule(id, duration)
  return id
}

function schedule(id: string, remaining: number) {
  const timerId = setTimeout(dismiss, remaining, id)
  timers.set(id, { remaining, startedAt: performance.now(), timerId })
}

export function dismiss(id: string) {
  clearTimeout(timers.get(id)?.timerId)
  timers.delete(id)
  toasts.value = toasts.value.filter(t => t.id !== id)
}

export function pauseToast(id: string) {
  const entry = timers.get(id)
  if (!entry)
    return
  clearTimeout(entry.timerId)
  entry.remaining = Math.max(0, entry.remaining - (performance.now() - entry.startedAt))
  timers.set(id, { ...entry, timerId: -1 as unknown as ReturnType<typeof setTimeout> })
}

export function resumeToast(id: string) {
  const entry = timers.get(id)
  if (!entry || entry.remaining <= 0) {
    dismiss(id)
    return
  }
  schedule(id, entry.remaining)
}

export const toast = {
  error: (msg: string, duration?: number) => add(msg, 'error', duration),
  success: (msg: string, duration?: number) => add(msg, 'success', duration),
  info: (msg: string, duration?: number) => add(msg, 'info', duration),
}

export function useToast() {
  return { toasts, toast, dismiss, pauseToast, resumeToast }
}
