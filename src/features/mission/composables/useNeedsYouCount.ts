import { computed } from 'vue'
import { usePendingPermissions } from '@/composables/usePendingPermissions'
import { useAgents } from '@/features/agents'
import { useTasks } from '@/features/pipeline'
import { rankNextThings } from './useNextThing'

export function useNeedsYouCount() {
  const { tasks } = useTasks({ autoStart: false })
  const { items } = usePendingPermissions(tasks)
  const { agents } = useAgents({ autoStart: false })
  return computed(() => rankNextThings(items.value, tasks.value, agents.value).length)
}
