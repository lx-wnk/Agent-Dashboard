import type { InjectionKey } from 'vue'
import type { usePendingPermissions } from './usePendingPermissions'

// How a widget opens a task: App.vue owns navigation and provides this.
export const OPEN_TASK: InjectionKey<(taskId: string) => void> = Symbol('openTask')

// App.vue owns the one usePendingPermissions(tasks) instance and provides it —
// a widget-local call would open a second cache that can disagree with the
// title count while its own fetch is still in flight (one owner per piece of state).
export const PENDING_PERMISSIONS: InjectionKey<ReturnType<typeof usePendingPermissions>> = Symbol('pendingPermissions')
