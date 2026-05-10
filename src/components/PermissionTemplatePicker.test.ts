import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PermissionTemplatePicker from './PermissionTemplatePicker.vue'

describe('permissionTemplatePicker', () => {
  it('selects a template on click', async () => {
    const wrapper = mount(PermissionTemplatePicker, { props: { modelValue: null } })
    const buttons = wrapper.findAll('button')
    expect(buttons.length).toBe(4)
    await buttons[0].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe('research_only')
  })

  it('deselects current template on re-click', async () => {
    const wrapper = mount(PermissionTemplatePicker, { props: { modelValue: 'research_only' } })
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBeNull()
  })
})
