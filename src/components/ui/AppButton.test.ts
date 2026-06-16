import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppButton from './AppButton.vue'

describe('appButton', () => {
  it('renders slot content', () => {
    const wrapper = mount(AppButton, { slots: { default: 'Click me' } })
    expect(wrapper.text()).toBe('Click me')
  })

  it('renders a button element', () => {
    const wrapper = mount(AppButton)
    expect(wrapper.element.tagName).toBe('BUTTON')
  })

  it('primary variant uses bg-accent', () => {
    const wrapper = mount(AppButton, { props: { variant: 'primary' } })
    expect(wrapper.classes()).toContain('bg-accent')
    expect(wrapper.classes()).toContain('text-white')
  })

  it('primary variant has border-0 (not outline)', () => {
    const wrapper = mount(AppButton, { props: { variant: 'primary' } })
    expect(wrapper.classes()).toContain('border-0')
    expect(wrapper.classes()).not.toContain('border-line-strong')
  })

  it('success variant uses bg-success', () => {
    const wrapper = mount(AppButton, { props: { variant: 'success' } })
    expect(wrapper.classes()).toContain('bg-success')
    expect(wrapper.classes()).toContain('text-white')
  })

  it('danger variant uses bg-danger', () => {
    const wrapper = mount(AppButton, { props: { variant: 'danger' } })
    expect(wrapper.classes()).toContain('bg-danger')
    expect(wrapper.classes()).toContain('text-white')
  })

  it('info variant uses bg-info', () => {
    const wrapper = mount(AppButton, { props: { variant: 'info' } })
    expect(wrapper.classes()).toContain('bg-info')
    expect(wrapper.classes()).toContain('text-white')
  })

  it('secondary variant uses bg-raised', () => {
    const wrapper = mount(AppButton, { props: { variant: 'secondary' } })
    expect(wrapper.classes()).toContain('bg-raised')
  })

  it('ghost variant is transparent', () => {
    const wrapper = mount(AppButton, { props: { variant: 'ghost' } })
    expect(wrapper.classes()).toContain('bg-transparent')
  })

  it('outline variant has border and border-line-strong', () => {
    const wrapper = mount(AppButton, { props: { variant: 'outline' } })
    expect(wrapper.classes()).toContain('border')
    expect(wrapper.classes()).toContain('border-line-strong')
    expect(wrapper.classes()).toContain('text-fg-soft')
  })

  it('outline variant does not have border-0', () => {
    const wrapper = mount(AppButton, { props: { variant: 'outline' } })
    expect(wrapper.classes()).not.toContain('border-0')
  })

  it('focus ring uses ring-accent', () => {
    const wrapper = mount(AppButton)
    expect(wrapper.classes()).toContain('focus-visible:ring-accent')
  })

  it('applies sm size classes', () => {
    const wrapper = mount(AppButton, { props: { size: 'sm' } })
    expect(wrapper.classes()).toContain('px-2.5')
    expect(wrapper.classes()).toContain('py-1')
    expect(wrapper.classes()).toContain('text-xs')
  })

  it('applies md size classes by default', () => {
    const wrapper = mount(AppButton)
    expect(wrapper.classes()).toContain('px-3.5')
    expect(wrapper.classes()).toContain('py-1.5')
    expect(wrapper.classes()).toContain('text-sm')
  })

  it('is disabled when disabled prop is true', async () => {
    const wrapper = mount(AppButton, { props: { disabled: true } })
    expect((wrapper.element as HTMLButtonElement).disabled).toBe(true)
  })

  it('defaults variant to secondary', () => {
    const wrapper = mount(AppButton)
    expect(wrapper.classes()).toContain('bg-raised')
  })
})
