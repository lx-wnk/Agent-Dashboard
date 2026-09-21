import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

vi.mock('@/composables/usePendingPermissions', () => ({ usePendingPermissions: () => ({ items: ref([]) }) }))
vi.mock('@/features/agents', () => ({ useAgents: () => ({ agents: ref([]) }) }))
vi.mock('@/features/pipeline', () => ({ useTasks: () => ({ tasks: ref([]) }) }))
vi.mock('../composables/useNextThing', () => ({ rankNextThings: () => [{}, {}] }))

const { useNeedsYouCount } = await import('../composables/useNeedsYouCount')

describe('useNeedsYouCount', () => {
  it('counts the ranked queue', () => {
    expect(useNeedsYouCount().value).toBe(2)
  })
})
