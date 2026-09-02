import type { Ref } from 'vue'
import type { Capability, Grant } from '@/features/settings/composables/useGrants'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import GrantSettings from '@/features/settings/components/GrantSettings.vue'
import { useGrants } from '@/features/settings/composables/useGrants'
import { formatDateTime } from '@/utils/format'

vi.mock('@/features/settings/composables/useGrants', async () => {
  const actual = await vi.importActual<typeof import('@/features/settings/composables/useGrants')>('@/features/settings/composables/useGrants')
  return {
    ...actual,
    useGrants: vi.fn(),
  }
})

const baseGrant: Grant = {
  id: 'g1',
  capabilityName: 'bash.exec',
  contextKind: 'global',
  contextRef: '',
  pattern: '*',
  mode: 'allow',
  limitCount: 0,
  limitWindowSeconds: 0,
  expiresAt: null,
  grantedBy: 'alex',
  grantedAt: '2026-01-01T00:00:00Z',
  revokedAt: null,
  revokedBy: '',
  reason: '',
  nodeId: '',
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
  let loading: Ref<boolean>
  let createGrant: ReturnType<typeof vi.fn>
  let revokeGrant: ReturnType<typeof vi.fn>

  beforeEach(() => {
    grants = ref([{ ...baseGrant }])
    loading = ref(false)
    capabilities = ref([
      { id: 'c1', name: 'bash.exec', class: 'shell', enforceableBy: [], requiresPattern: true, reversible: false, description: '' },
    ])
    createGrant = vi.fn(async (input: Record<string, unknown>) => {
      const created = { ...baseGrant, id: 'g-new', capabilityName: input.capabilityName as string }
      grants.value = [created, ...grants.value]
      return created
    })
    revokeGrant = vi.fn(async (id: string) => {
      grants.value = grants.value.map(g => g.id === id ? { ...g, revokedAt: '2026-02-01T00:00:00Z', revokedBy: 'alex' } : g)
    })

    vi.mocked(useGrants).mockReturnValue({
      grants,
      capabilities,
      loading,
      error: ref(null),
      fetchGrants: vi.fn(),
      createGrant,
      revokeGrant,
    } as unknown as ReturnType<typeof useGrants>)
  })

  it('renders the grant list, including a revoked row and a legacy-migration row', () => {
    grants.value = [
      { ...baseGrant, id: 'g1' },
      { ...baseGrant, id: 'g2', revokedAt: '2026-02-01T00:00:00Z', revokedBy: 'alex' },
      { ...baseGrant, id: 'g3', grantedBy: 'migration:legacy' },
    ]
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="grant-row-g1"]').text()).toContain('bash.exec')
    const revokedRow = wrapper.get('[data-testid="grant-row-g2"]')
    expect(revokedRow.text()).toContain('Revoked')
    expect(revokedRow.text()).toContain('alex')
    expect(wrapper.get('[data-testid="grant-row-g3"]').text()).toContain('Legacy migration')
  })

  it('marks a grant on a capability no enforcement point reads as not enforced', () => {
    capabilities.value = [
      { id: 'c1', name: 'bash.exec', class: 'shell', enforceableBy: [], requiresPattern: true, reversible: false, description: '' },
      { id: 'c2', name: 'memory.read', class: 'resource', enforceableBy: ['server'], requiresPattern: false, reversible: true, description: '' },
    ]
    grants.value = [
      { ...baseGrant, id: 'g1', capabilityName: 'bash.exec' },
      { ...baseGrant, id: 'g2', capabilityName: 'memory.read' },
      { ...baseGrant, id: 'g3', capabilityName: 'not.in.catalogue' },
    ]
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="grant-enforcement-g1"]').text()).toBe('none')
    expect(wrapper.get('[data-testid="grant-enforcement-g2"]').text()).toBe('server')
    expect(wrapper.get('[data-testid="grant-enforcement-g3"]').text()).toBe('unknown')
  })

  // The tone carries the meaning at a glance. A green "none" reads as enforced
  // and defeats the column, so the badge classes are pinned, not just the text.
  it('reserves the success tone for an actually enforced grant', () => {
    capabilities.value = [
      { id: 'c1', name: 'bash.exec', class: 'shell', enforceableBy: [], requiresPattern: true, reversible: false, description: '' },
      { id: 'c2', name: 'memory.read', class: 'resource', enforceableBy: ['server'], requiresPattern: false, reversible: true, description: '' },
    ]
    grants.value = [
      { ...baseGrant, id: 'g1', capabilityName: 'bash.exec' },
      { ...baseGrant, id: 'g2', capabilityName: 'memory.read' },
      { ...baseGrant, id: 'g3', capabilityName: 'not.in.catalogue' },
    ]
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    for (const id of ['g1', 'g3']) {
      const badge = wrapper.get(`[data-testid="grant-enforcement-${id}"]`)
      expect(badge.classes()).toContain('bg-warning-soft')
      expect(badge.classes()).not.toContain('bg-success-soft')
    }
    const enforced = wrapper.get('[data-testid="grant-enforcement-g2"]')
    expect(enforced.classes()).toContain('bg-success-soft')
    expect(enforced.classes()).not.toContain('bg-warning-soft')
  })

  it('keeps a live region mounted across the load, announcing by content change', async () => {
    loading.value = true
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    const region = wrapper.get('[data-testid="grant-status"]')
    expect(region.attributes('role')).toBe('status')
    expect(region.attributes('aria-live')).toBe('polite')
    expect(wrapper.get('[data-testid="grant-loading"]').text()).toContain('Loading grants')

    loading.value = false
    await flushPromises()

    expect(wrapper.get('[data-testid="grant-status"]').attributes('role')).toBe('status')
    expect(wrapper.find('[data-testid="grant-loading"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="grant-row-g1"]').exists()).toBe(true)
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

  it('marks a revoked grant next to its mode, not only in the status column', () => {
    grants.value = [
      { ...baseGrant, id: 'g1' },
      { ...baseGrant, id: 'g2', revokedAt: '2026-02-01T00:00:00Z', revokedBy: 'alex' },
    ]
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    expect(wrapper.get('[data-testid="grant-mode-revoked-g2"]').text()).toContain('Revoked')
    expect(wrapper.find('[data-testid="grant-mode-revoked-g1"]').exists()).toBe(false)
  })

  it('merges granted-by and status into one Provenance column', () => {
    grants.value = [
      { ...baseGrant, id: 'g1', grantedBy: 'cli:alexanderwink' },
      { ...baseGrant, id: 'g2', grantedBy: 'cli:alexanderwink', revokedAt: '2026-02-01T00:00:00Z', revokedBy: 'alex' },
      { ...baseGrant, id: 'g3', grantedBy: 'migration:legacy' },
    ]
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    const activeRow = wrapper.get('[data-testid="grant-row-g1"]').text()
    expect(activeRow).toContain('cli:alexanderwink')
    expect(activeRow).not.toContain('Revoked')

    const revokedRow = wrapper.get('[data-testid="grant-row-g2"]').text()
    expect(revokedRow).toContain('cli:alexanderwink')
    expect(revokedRow).toContain(`Revoked ${formatDateTime('2026-02-01T00:00:00Z')} by alex`)

    const legacyRow = wrapper.get('[data-testid="grant-row-g3"]').text()
    expect(legacyRow).toContain('Legacy migration')
  })

  it('drops one column when merging Granted By and Status into Provenance', () => {
    const wrapper = mount(GrantSettings, { attachTo: document.body })

    expect(wrapper.findAll('th')).toHaveLength(9)
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
