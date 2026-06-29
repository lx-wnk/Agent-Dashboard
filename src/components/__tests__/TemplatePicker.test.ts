import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import TemplatePicker from '../TemplatePicker.vue'

const templates = [
  { id: '1', name: 'greet', body: 'Hello {{name}}, welcome to {{place}}!', createdAt: '' },
  { id: '2', name: 'simple', body: 'No placeholders here.', createdAt: '' },
]

vi.mock('../../composables/usePromptTemplates', () => ({
  usePromptTemplates: () => ({
    templates: ref(templates),
    create: vi.fn(),
    remove: vi.fn(),
  }),
}))

describe('templatePicker', () => {
  it('renders template names in the selector', () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' } })
    const options = wrapper.findAll('option')
    expect(options.some(o => o.text() === 'greet')).toBe(true)
    expect(options.some(o => o.text() === 'simple')).toBe(true)
  })

  it('emits placeholder inputs when a template with tokens is selected', async () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' } })
    await wrapper.find('select').setValue('1')
    await nextTick()
    // Two placeholder inputs: name and place
    expect(wrapper.findAll('input[data-placeholder]')).toHaveLength(2)
  })

  it('emits resolved text when placeholders are filled', async () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' } })
    await wrapper.find('select').setValue('1')
    await nextTick()

    const inputs = wrapper.findAll('input[data-placeholder]')
    await inputs[0].setValue('Alice')
    await inputs[1].setValue('Wonderland')

    await wrapper.find('[data-apply]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe(
      'Hello Alice, welcome to Wonderland!',
    )
  })

  it('emits template body directly when no placeholders', async () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' } })
    await wrapper.find('select').setValue('2')
    await nextTick()

    await wrapper.find('[data-apply]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe('No placeholders here.')
  })
})
