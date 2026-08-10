import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppInput from './AppInput.vue'

describe('appInput', () => {
  it('forwards a caller-supplied aria-describedby to the input', () => {
    const wrapper = mount(AppInput, { attrs: { 'aria-describedby': 'outside-hint' } })
    expect(wrapper.get('input').attributes('aria-describedby')).toBe('outside-hint')
  })

  it('keeps the caller-supplied description alongside its own error message', () => {
    const wrapper = mount(AppInput, {
      props: { error: 'Nope' },
      attrs: { 'aria-describedby': 'outside-hint' },
    })
    const errorId = wrapper.get('p[role="status"]').attributes('id')
    expect(wrapper.get('input').attributes('aria-describedby')).toBe(`outside-hint ${errorId}`)
  })

  it('describes nothing when neither side supplies a description', () => {
    const wrapper = mount(AppInput)
    expect(wrapper.get('input').attributes('aria-describedby')).toBeUndefined()
  })

  it('forwards a caller-supplied aria-describedby to the textarea', () => {
    const wrapper = mount(AppInput, {
      props: { type: 'textarea' },
      attrs: { 'aria-describedby': 'outside-hint' },
    })
    expect(wrapper.get('textarea').attributes('aria-describedby')).toBe('outside-hint')
  })
})
