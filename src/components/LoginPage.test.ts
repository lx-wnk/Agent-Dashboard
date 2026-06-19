import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import LoginPage from './LoginPage.vue'

function setSearch(search: string) {
  window.history.replaceState({}, '', `/${search}`)
}

describe('loginPage', () => {
  afterEach(() => setSearch(''))

  it('exposes a <main> landmark', () => {
    const wrapper = mount(LoginPage)
    expect(wrapper.find('main').exists()).toBe(true)
  })

  it('renders the title as a single <h1>', () => {
    const wrapper = mount(LoginPage)
    const headings = wrapper.findAll('h1')
    expect(headings).toHaveLength(1)
    expect(headings[0].text()).toBe('Claude Agent Dashboard')
  })

  it('shows no alert without an error param', () => {
    const wrapper = mount(LoginPage)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('announces the mapped label for ?error=auth_failed via role="alert"', () => {
    setSearch('?error=auth_failed')
    const wrapper = mount(LoginPage)
    const alert = wrapper.find('[role="alert"]')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toBe('Authentication failed')
  })

  it('falls back to generic copy for an unknown error code', () => {
    setSearch('?error=boom')
    const wrapper = mount(LoginPage)
    const alert = wrapper.find('[role="alert"]')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toBe('Login failed. Please try again.')
  })

  it('focusLogin moves focus to the login link', async () => {
    const wrapper = mount(LoginPage, { attachTo: document.body })
    wrapper.vm.focusLogin()
    await wrapper.vm.$nextTick()
    expect(document.activeElement).toBe(wrapper.find('a[href="/api/auth/login"]').element)
    wrapper.unmount()
  })
})
