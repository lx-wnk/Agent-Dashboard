import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppTopbar from './AppTopbar.vue'

describe('appTopbar', () => {
  it('renders the view title', () => {
    const w = mount(AppTopbar, { props: { activeView: 'cost', searchQuery: '' } })
    expect(w.text()).toContain('Cost')
  })

  it('emits update:searchQuery on input', async () => {
    const w = mount(AppTopbar, { props: { activeView: 'dashboard', searchQuery: '' } })
    await w.get('input').setValue('foo')
    expect(w.emitted('update:searchQuery')![0]).toEqual(['foo'])
  })

  it('renders the cta slot', () => {
    const w = mount(AppTopbar, {
      props: { activeView: 'dashboard', searchQuery: '' },
      slots: { cta: '<button>+ New Agent</button>' },
    })
    expect(w.text()).toContain('+ New Agent')
  })

  it('emits openSettings when the gear button is clicked', async () => {
    const w = mount(AppTopbar, { props: { activeView: 'dashboard', searchQuery: '' } })
    await w.get('button[aria-label="Settings"]').trigger('click')
    expect(w.emitted('openSettings')).toBeTruthy()
  })
})
