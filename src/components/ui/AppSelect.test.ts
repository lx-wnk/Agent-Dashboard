import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppSelect from './AppSelect.vue'

const options = [
  { value: 'a', label: 'Option A' },
  { value: 'b', label: 'Option B' },
  { value: 'c', label: 'Option C' },
]

describe('AppSelect', () => {
  it('renders all options', () => {
    const wrapper = mount(AppSelect, {
      props: { modelValue: 'a', options },
    })
    const optionEls = wrapper.findAll('option')
    expect(optionEls).toHaveLength(3)
    expect(optionEls[0].text()).toBe('Option A')
    expect(optionEls[1].text()).toBe('Option B')
    expect(optionEls[2].text()).toBe('Option C')
  })

  it('reflects modelValue as the selected option', () => {
    const wrapper = mount(AppSelect, {
      props: { modelValue: 'b', options },
    })
    const select = wrapper.find('select')
    expect((select.element as HTMLSelectElement).value).toBe('b')
  })

  it('emits update:modelValue on change', async () => {
    const wrapper = mount(AppSelect, {
      props: { modelValue: 'a', options },
    })
    const select = wrapper.find('select')
    ;(select.element as HTMLSelectElement).value = 'c'
    await select.trigger('change')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['c'])
  })

  it('forwards id prop to the select element', () => {
    const wrapper = mount(AppSelect, {
      props: { modelValue: 'a', options, id: 'my-select' },
    })
    expect(wrapper.find('select').attributes('id')).toBe('my-select')
  })

  it('forwards aria-label prop', () => {
    const wrapper = mount(AppSelect, {
      props: { modelValue: 'a', options, ariaLabel: 'Choose option' },
    })
    expect(wrapper.find('select').attributes('aria-label')).toBe('Choose option')
  })

  it('disables the select when disabled prop is true', () => {
    const wrapper = mount(AppSelect, {
      props: { modelValue: 'a', options, disabled: true },
    })
    expect((wrapper.find('select').element as HTMLSelectElement).disabled).toBe(true)
  })

  it('is not disabled by default', () => {
    const wrapper = mount(AppSelect, {
      props: { modelValue: 'a', options },
    })
    expect((wrapper.find('select').element as HTMLSelectElement).disabled).toBe(false)
  })

  it('works with numeric option values', () => {
    const numOptions = [
      { value: 1, label: 'One' },
      { value: 2, label: 'Two' },
    ]
    const wrapper = mount(AppSelect, {
      props: { modelValue: 1, options: numOptions },
    })
    const optionEls = wrapper.findAll('option')
    expect(optionEls).toHaveLength(2)
    expect(optionEls[0].attributes('value')).toBe('1')
  })
})
