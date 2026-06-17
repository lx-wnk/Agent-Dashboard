import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppChip from './AppChip.vue'

describe('appChip', () => {
  it('renders slot content', () => {
    const wrapper = mount(AppChip, { slots: { default: 'Running' } })
    expect(wrapper.text()).toBe('Running')
  })

  it('renders a span element', () => {
    const wrapper = mount(AppChip, { slots: { default: 'x' } })
    expect(wrapper.element.tagName).toBe('SPAN')
  })

  it('applies success tone classes', () => {
    const wrapper = mount(AppChip, { props: { tone: 'success' } })
    expect(wrapper.classes()).toContain('bg-success-soft')
    expect(wrapper.classes()).toContain('text-success-text')
  })

  it('applies warning tone classes', () => {
    const wrapper = mount(AppChip, { props: { tone: 'warning' } })
    expect(wrapper.classes()).toContain('bg-warning-soft')
    expect(wrapper.classes()).toContain('text-warning-text')
  })

  it('applies danger tone classes', () => {
    const wrapper = mount(AppChip, { props: { tone: 'danger' } })
    expect(wrapper.classes()).toContain('bg-danger-soft')
    expect(wrapper.classes()).toContain('text-danger-text')
  })

  it('applies info tone classes', () => {
    const wrapper = mount(AppChip, { props: { tone: 'info' } })
    expect(wrapper.classes()).toContain('bg-info-soft')
    expect(wrapper.classes()).toContain('text-info-text')
  })

  it('defaults to neutral tone', () => {
    const wrapper = mount(AppChip)
    expect(wrapper.classes()).toContain('bg-neutral-soft')
    expect(wrapper.classes()).toContain('text-neutral-text')
  })

  it('applies border class when bordered (default)', () => {
    const wrapper = mount(AppChip, { props: { tone: 'success' } })
    expect(wrapper.classes()).toContain('border')
    expect(wrapper.classes()).toContain('border-success-line')
  })

  it('omits border class when bordered=false', () => {
    const wrapper = mount(AppChip, { props: { bordered: false } })
    expect(wrapper.classes()).not.toContain('border')
  })

  it('applies font-mono when mono=true', () => {
    const wrapper = mount(AppChip, { props: { mono: true } })
    expect(wrapper.classes()).toContain('font-mono')
  })

  it('does not apply font-mono by default', () => {
    const wrapper = mount(AppChip)
    expect(wrapper.classes()).not.toContain('font-mono')
  })

  it('applies uppercase when uppercase=true', () => {
    const wrapper = mount(AppChip, { props: { uppercase: true } })
    expect(wrapper.classes()).toContain('uppercase')
  })

  it('does not apply uppercase by default', () => {
    const wrapper = mount(AppChip)
    expect(wrapper.classes()).not.toContain('uppercase')
  })
})
