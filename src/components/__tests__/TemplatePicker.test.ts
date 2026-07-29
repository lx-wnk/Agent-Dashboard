import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
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

// AppSelect (used for the template selector) is a custom listbox, not a
// native <select> — its panel teleports to <body> while open, so options are
// read through the trigger button + teleported [role="option"] elements
// instead of wrapper.findAll('option') / select.setValue().
interface SelectTrigger {
  trigger: (event: string) => Promise<unknown>
  attributes: (key: string) => string | undefined
}

async function openListbox(select: SelectTrigger): Promise<HTMLElement> {
  await select.trigger('click')
  return document.getElementById(select.attributes('aria-controls')!)!
}

function optionByLabel(panel: HTMLElement, label: string): HTMLElement {
  const match = Array.from(panel.querySelectorAll('[role="option"]'))
    .find(el => el.textContent?.trim() === label)
  if (!match)
    throw new Error(`No option with label "${label}" found`)
  return match as HTMLElement
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('templatePicker', () => {
  it('renders template names in the selector', async () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' }, attachTo: document.body })
    const panel = await openListbox(wrapper.get('[role="combobox"]'))
    const labels = Array.from(panel.querySelectorAll('[role="option"]')).map(el => el.textContent?.trim())
    expect(labels).toContain('greet')
    expect(labels).toContain('simple')
  })

  it('emits placeholder inputs when a template with tokens is selected', async () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' }, attachTo: document.body })
    const panel = await openListbox(wrapper.get('[role="combobox"]'))
    optionByLabel(panel, 'greet').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    // Two placeholder inputs: name and place
    expect(wrapper.findAll('input[data-placeholder]')).toHaveLength(2)
  })

  it('emits resolved text when placeholders are filled', async () => {
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' }, attachTo: document.body })
    const panel = await openListbox(wrapper.get('[role="combobox"]'))
    optionByLabel(panel, 'greet').dispatchEvent(new MouseEvent('click', { bubbles: true }))
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
    const wrapper = mount(TemplatePicker, { props: { modelValue: '' }, attachTo: document.body })
    const panel = await openListbox(wrapper.get('[role="combobox"]'))
    optionByLabel(panel, 'simple').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()

    await wrapper.find('[data-apply]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toBe('No placeholders here.')
  })
})
