import type { InjectionKey } from 'vue'

// How a widget opens a task: App.vue owns navigation and provides this.
export const OPEN_TASK: InjectionKey<(taskId: string) => void> = Symbol('openTask')
