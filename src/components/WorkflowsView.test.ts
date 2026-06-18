import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import WorkflowsView from './WorkflowsView.vue'

function emptySankey() {
  return { nodes: [], links: [], meta: { sessionCount: 0, callCount: 0 } }
}

function makeFetchMock(sessions: unknown[] = []) {
  return vi.fn().mockImplementation((url: string) => {
    if (String(url).includes('/api/sessions')) {
      return Promise.resolve({ ok: true, json: async () => sessions })
    }
    return Promise.resolve({ ok: true, json: async () => emptySankey() })
  })
}

describe('workflowsView', () => {
  it('renders the three static tab buttons (no Session DAG initially)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => emptySankey(),
    }))
    const wrapper = mount(WorkflowsView)
    await flushPromises()
    for (const label of ['Sankey', 'Spawn Tree', 'Co-occurrence'])
      expect(wrapper.text()).toContain(label)
    // Session DAG is not visible without a session
    const dagButton = wrapper.findAll('button[role="tab"]').find(b => b.text() === 'Session DAG')
    expect(dagButton).toBeUndefined()
  })

  it('shows the empty sankey state when the backend returns no data', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => emptySankey(),
    }))
    const wrapper = mount(WorkflowsView)
    await flushPromises()
    expect(wrapper.text()).toContain('No tool calls found in this window.')
  })

  it('does not render the DAG tab button when sessionId is empty', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => emptySankey(),
    }))
    const wrapper = mount(WorkflowsView)
    await flushPromises()
    const dagButton = wrapper.findAll('button[role="tab"]').find(b => b.text() === 'Session DAG')
    expect(dagButton).toBeUndefined()
  })

  it('renders session dropdown options from useSessions', async () => {
    const mockSessions = [
      {
        sessionId: 'abc12345-0000-0000-0000-000000000000',
        projectName: 'my-project',
        firstPrompt: 'Build a new feature',
        isRunning: false,
        projectPath: '/tmp/my-project',
        lastModified: '2026-06-01T10:00:00Z',
        model: null,
        lastResponse: null,
        totalInputTokens: 0,
        totalOutputTokens: 0,
        costEstimate: 0,
      },
      {
        sessionId: 'def67890-0000-0000-0000-000000000000',
        projectName: 'other-project',
        firstPrompt: null,
        isRunning: true,
        projectPath: '/tmp/other-project',
        lastModified: '2026-06-01T11:00:00Z',
        model: null,
        lastResponse: null,
        totalInputTokens: 0,
        totalOutputTokens: 0,
        costEstimate: 0,
      },
    ]
    vi.stubGlobal('fetch', makeFetchMock(mockSessions))
    const wrapper = mount(WorkflowsView)
    await flushPromises()

    const select = wrapper.find('select')
    expect(select.exists()).toBe(true)

    const options = select.findAll('option')
    // First option is the placeholder
    expect(options[0].text()).toContain('Select session')
    expect(options[0].attributes('value')).toBe('')

    // Second option: projectName · firstPrompt (no running suffix)
    expect(options[1].text()).toContain('my-project')
    expect(options[1].text()).toContain('Build a new feature')
    expect(options[1].text()).not.toContain('(running)')

    // Third option: projectName · first 8 chars of sessionId + (running)
    expect(options[2].text()).toContain('other-project')
    expect(options[2].text()).toContain('def67890')
    expect(options[2].text()).toContain('(running)')
  })

  it('shows Session DAG tab and makes it active when a session is selected from the dropdown', async () => {
    const mockSessions = [
      {
        sessionId: 'abc12345-0000-0000-0000-000000000000',
        projectName: 'my-project',
        firstPrompt: 'Some prompt',
        isRunning: false,
        projectPath: '/tmp/my-project',
        lastModified: '2026-06-01T10:00:00Z',
        model: null,
        lastResponse: null,
        totalInputTokens: 0,
        totalOutputTokens: 0,
        costEstimate: 0,
      },
    ]
    vi.stubGlobal('fetch', makeFetchMock(mockSessions))
    const wrapper = mount(WorkflowsView)
    await flushPromises()

    // Confirm DAG tab is absent initially (no session)
    const dagButtonBefore = wrapper.findAll('button[role="tab"]').find(b => b.text() === 'Session DAG')
    expect(dagButtonBefore).toBeUndefined()

    // Select a session via the dropdown
    const select = wrapper.find('select')
    await select.setValue('abc12345-0000-0000-0000-000000000000')
    await select.trigger('change')
    await flushPromises()

    // DAG tab should now be present
    const dagButton = wrapper.findAll('button[role="tab"]').find(b => b.text() === 'Session DAG')
    expect(dagButton).toBeTruthy()

    // DAG tab should be active (has the active class)
    expect(dagButton!.classes()).toContain('bg-blue-600')
  })

  it('hides Session DAG tab and returns to Sankey after Reset', async () => {
    const mockSessions = [
      {
        sessionId: 'abc12345-0000-0000-0000-000000000000',
        projectName: 'my-project',
        firstPrompt: 'Some prompt',
        isRunning: false,
        projectPath: '/tmp/my-project',
        lastModified: '2026-06-01T10:00:00Z',
        model: null,
        lastResponse: null,
        totalInputTokens: 0,
        totalOutputTokens: 0,
        costEstimate: 0,
      },
    ]
    vi.stubGlobal('fetch', makeFetchMock(mockSessions))
    const wrapper = mount(WorkflowsView)
    await flushPromises()

    // Select a session to show the DAG tab
    const select = wrapper.find('select')
    await select.setValue('abc12345-0000-0000-0000-000000000000')
    await select.trigger('change')
    await flushPromises()

    // Confirm DAG tab is active
    const dagButtonActive = wrapper.findAll('button[role="tab"]').find(b => b.text() === 'Session DAG')
    expect(dagButtonActive).toBeTruthy()
    expect(dagButtonActive!.classes()).toContain('bg-blue-600')

    // Click Reset
    const resetButton = wrapper.find('button[type="button"]:not([role="tab"])')
    await resetButton.trigger('click')
    await flushPromises()

    // DAG tab should be gone
    const dagButtonAfter = wrapper.findAll('button[role="tab"]').find(b => b.text() === 'Session DAG')
    expect(dagButtonAfter).toBeUndefined()

    // Sankey tab should be active
    const sankeyButton = wrapper.findAll('button[role="tab"]').find(b => b.text() === 'Sankey')
    expect(sankeyButton).toBeTruthy()
    expect(sankeyButton!.classes()).toContain('bg-blue-600')
  })
})
