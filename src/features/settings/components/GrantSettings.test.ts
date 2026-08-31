import type { Ref } from 'vue'
import type { Capability, Grant } from '@/features/settings/composables/useGrants'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import GrantSettings from '@/features/settings/components/GrantSettings.vue'
import { useGrants } from '@/features/settings/composables/useGrants'

vi.mock('@/features/settings/composables/useGrants', async () => {
  const actual = await vi.importActual<typeof import('@/features/settings/composables/useGrants')>('@/features/settings/composables/useGrants')
  return {
    ...actual,
    useGrants: vi.fn(),
  }
})

const baseGrant: Grant = {
  id: 'g1',
  capability_name: 'bash.exec',
  context_kind: 'global',
  context_ref: '',
  pattern: '*',
  mode: 'allow',
  limit_count: 0,
  limit_window_seconds: 0,
  expires_at: null,
  granted_by: 'alex',
  granted_at: '2026-01-01T00:00:00Z',
  revoked_at: null,
  revoked_by: '',
  reason: '',
}

async function selectOption(wrapper: ReturnType<typeof mount>, triggerTestId: string, label: string): Promise<void> {
  await wrapper.get(`[data-testid="${triggerTestId}"]`).trigger('click')
  const option = Array.from(document.querySelectorAll('[role="option"]')).find(el => el.textContent?.trim() === label)
  if (!option)
    throw new Error(`option "${label}" not found in panel`)
  option.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('grantSettings', () => {
  let grants: Ref<Grant[]>
  let capabilities: Ref<Capability[]>
  let createGrant: ReturnType<typeof vi.fn>
  let revokeGrant: ReturnType<typeof vi.fn>

  beforeEach(() => {
    grants = ref([{ ...baseGrant }])
    capabilities = ref([
      { id: 'c1', name: 'bash.exec', class: 'shell', enforceable_by: [], requires_pattern: true, reversible: false, description: '' },
    ])
    createGrant = vi.fn(async (input: Record<string, unknown>) => {
      const created = { ...baseGrant, id: 'g-new', capability_name: input.capabilityName as string }
      grants.value = [created, ...grants.value]
      return created
    })
    revokeGrant = vi.fn(async (id: string) => {
      grants.value = grants.value.map(g => g.id === id ? { ...g, revoked_at: '2026-02-01T00:00:00Z', revoked_by: 'alex' } : g)
    })

    vi.mocked(useGrants).mockReturnValue({
      grants,
      capabilities,
      loading: ref(false),
      error: ref(null),
      fetchGrants: vi.fn(),
      createGrant,
      revokeGrant,
    } as unknown as ReturnType<typeof useGrants>)
  })

  it('renders the grant list, including a revoked row and a legacy-migration row', () => {
    grants.value = [
      { ...baseGrant, id: 'g1' },
      { ...baseGrant, id: 'g2', revoked_at: '2026-02-01T00:00:00Z', revoked_by: 'alex' },
      { ...baseGrant, id: 'g3', granted_by: 'migration:legacy' },
    ]
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="grant-row-g1"]').text()).toContain('bash.exec')
    const revokedRow = wrapper.get('[data-testid="grant-row-g2"]')
    expect(revokedRow.text()).toContain('Revoked')
    expect(revokedRow.text()).toContain('alex')
    expect(wrapper.get('[data-testid="grant-row-g3"]').text()).toContain('Legacy migration')
  })

  it('posts the right body when creating a grant', async () => {
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="grant-new"]').trigger('click')
    await selectOption(wrapper, 'grant-capability', 'bash.exec')
    await wrapper.get('[data-testid="grant-pattern"]').setValue('git status*')
    await wrapper.get('[data-testid="grant-submit"]').trigger('click')
    await flushPromises()

    expect(createGrant).toHaveBeenCalledWith({
      capabilityName: 'bash.exec',
      contextKind: 'global',
      contextRef: '',
      pattern: 'git status*',
      mode: 'allow',
      limitCount: 0,
      limitWindowSeconds: 0,
      reason: '',
    })
  })

  it('clears and disables the context ref when the context kind is global', async () => {
    const wrapper = mount(GrantSettings, { attachTo: document.body })
    await wrapper.get('[data-testid="grant-new"]').trigger('click')

    await wrapper.get('[data-testid="grant-context-ref"]').setValue('should-be-cleared')
    await selectOption(wrapper, 'grant-context-kind', 'task')
    await wrapper.get('[data-testid="grant-context-ref"]').setValue('task-123')
    await selectOption(wrapper, 'grant-context-kind', 'global')

    const refInput = wrapper.get('[data-testid="grant-context-ref"]').element as HTMLInputElement
    expect(refInput.value).toBe('')
    expect(refInput.disabled).toBe(true)
  })

  it('surfaces the server 400 message when creation is refused', async () => {
    createGrant.mockRejectedValueOnce(new Error('unknown capability "bogus" — see GET /api/capabilities for valid names'))
    const toastMod = await import('@/composables/useToast')
    const errorSpy = vi.spyOn(toastMod.toast, 'error')

    const wrapper = mount(GrantSettings, { attachTo: document.body })
    await wrapper.get('[data-testid="grant-new"]').trigger('click')
    await selectOption(wrapper, 'grant-capability', 'bash.exec')
    await wrapper.get('[data-testid="grant-submit"]').trigger('click')
    await flushPromises()

    expect(errorSpy).toHaveBeenCalledWith(expect.stringContaining('unknown capability'))
  })

  it('revokes a grant via DELETE and reflects the revoked state', async () => {
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    await wrapper.get('[data-testid="grant-revoke-g1"]').trigger('click')
    await wrapper.get('[data-testid="grant-revoke-confirm-g1"]').trigger('click')
    await flushPromises()

    expect(revokeGrant).toHaveBeenCalledWith('g1', undefined)
    expect(wrapper.get('[data-testid="grant-row-g1"]').text()).toContain('Revoked')
  })
})
