import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppTopbar from './AppTopbar.vue'

describe('appTopbar', () => {
  it('renders the view title', () => {
    const w = mount(AppTopbar, { props: { activeView: 'cost', searchQuery: '', live: true } })
    expect(w.text()).toContain('Cost')
  })

  it('emits update:searchQuery on input', async () => {
    const w = mount(AppTopbar, { props: { activeView: 'dashboard', searchQuery: '', live: true } })
    await w.get('input').setValue('foo')
    expect(w.emitted('update:searchQuery')![0]).toEqual(['foo'])
  })

  it('renders the cta slot', () => {
    const w = mount(AppTopbar, {
      props: { activeView: 'dashboard', searchQuery: '', live: true },
      slots: { cta: '<button>+ New Agent</button>' },
    })
    expect(w.text()).toContain('+ New Agent')
  })

  it('shows reconnecting state when not live', () => {
    const w = mount(AppTopbar, { props: { activeView: 'dashboard', searchQuery: '', live: false } })
    expect(w.text()).toContain('Reconnecting')
  })
})
