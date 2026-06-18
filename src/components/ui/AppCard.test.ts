import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppCard from './AppCard.vue'

describe('appCard', () => {
  it('renders slot content', () => {
    const wrapper = mount(AppCard, { slots: { default: 'hello' } })
    expect(wrapper.text()).toBe('hello')
  })

  it('renders a div element', () => {
    const wrapper = mount(AppCard)
    expect(wrapper.element.tagName).toBe('DIV')
  })

  it('defaults to bg-card surface', () => {
    const wrapper = mount(AppCard)
    expect(wrapper.classes()).toContain('bg-card')
  })

  it('app surface applies bg-app', () => {
    const wrapper = mount(AppCard, { props: { surface: 'app' } })
    expect(wrapper.classes()).toContain('bg-app')
    expect(wrapper.classes()).not.toContain('bg-card')
  })

  it('defaults to rounded-lg radius', () => {
    const wrapper = mount(AppCard)
    expect(wrapper.classes()).toContain('rounded-lg')
  })

  it('md radius applies rounded-md', () => {
    const wrapper = mount(AppCard, { props: { radius: 'md' } })
    expect(wrapper.classes()).toContain('rounded-md')
    expect(wrapper.classes()).not.toContain('rounded-lg')
  })

  it('always has border and border-line', () => {
    const wrapper = mount(AppCard)
    expect(wrapper.classes()).toContain('border')
    expect(wrapper.classes()).toContain('border-line')
  })

  it('non-interactive has no hover-border class', () => {
    const wrapper = mount(AppCard)
    expect(wrapper.classes()).not.toContain('hover:border-line-strong')
  })

  it('interactive adds hover:border-line-strong', () => {
    const wrapper = mount(AppCard, { props: { interactive: true } })
    expect(wrapper.classes()).toContain('hover:border-line-strong')
  })

  it('interactive without lift adds hover:shadow-card-hover', () => {
    const wrapper = mount(AppCard, { props: { interactive: true, lift: false } })
    expect(wrapper.classes()).toContain('hover:shadow-card-hover')
    expect(wrapper.classes()).not.toContain('hover:-translate-y-px')
  })

  it('interactive with lift adds hover:-translate-y-px', () => {
    const wrapper = mount(AppCard, { props: { interactive: true, lift: true } })
    expect(wrapper.classes()).toContain('hover:-translate-y-px')
    expect(wrapper.classes()).not.toContain('hover:shadow-card-hover')
  })

  it('non-interactive ignores lift', () => {
    const wrapper = mount(AppCard, { props: { interactive: false, lift: true } })
    expect(wrapper.classes()).not.toContain('hover:-translate-y-px')
    expect(wrapper.classes()).not.toContain('hover:shadow-card-hover')
  })
})
