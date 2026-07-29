import type { VueWrapper } from '@vue/test-utils'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { axe } from '../../utils/testA11y'
import AppSelect from './AppSelect.vue'

// AppSelect teleports its panel to <body> (real DOM is required there — the
// panel anchors to the trigger's getBoundingClientRect() and the codebase
// pattern for teleported content, documented in SpawnDialog.test.ts, is to
// query via document.querySelector rather than wrapper.find).

const options = [
  { value: 'a', label: 'Option A' },
  { value: 'b', label: 'Option B' },
  { value: 'c', label: 'Option C' },
]

const optionsWithDisabled = [
  { value: 'a', label: 'Option A' },
  { value: 'b', label: 'Option B', disabled: true },
  { value: 'c', label: 'Option C' },
]

let wrapper: VueWrapper | null = null

function mountSelect(props: Record<string, unknown>) {
  wrapper = mount(AppSelect, { props: props as any, attachTo: document.body })
  return wrapper
}

function panel(): HTMLElement | null {
  return document.querySelector('[role="listbox"]')
}

function optionEls(): HTMLElement[] {
  return Array.from(document.querySelectorAll('[role="option"]'))
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
})

describe('appSelect', () => {
  it('renders the selected option label on the trigger and no panel until opened', () => {
    const w = mountSelect({ modelValue: 'b', options })
    expect(w.get('button').text()).toContain('Option B')
    expect(panel()).toBeNull()
  })

  it('renders an empty trigger label when modelValue matches nothing', () => {
    const w = mountSelect({ modelValue: 'zzz', options })
    expect(w.get('button').text()).not.toMatch(/Option/)
  })

  it('forwards id, aria-label and disabled to the trigger button', () => {
    const w = mountSelect({ modelValue: 'a', options, id: 'my-select', ariaLabel: 'Choose option', disabled: true })
    const button = w.get('button')
    expect(button.attributes('id')).toBe('my-select')
    expect(button.attributes('aria-label')).toBe('Choose option')
    expect((button.element as HTMLButtonElement).disabled).toBe(true)
  })

  it('is not disabled by default', () => {
    const w = mountSelect({ modelValue: 'a', options })
    expect((w.get('button').element as HTMLButtonElement).disabled).toBe(false)
  })

  it('merges fallthrough class with the trigger classes instead of clobbering them', () => {
    const w = mountSelect({ 'modelValue': 'a', options, 'class': 'w-full', 'data-testid': 'my-select' })
    const button = w.get('button')
    expect(button.classes()).toContain('w-full')
    expect(button.classes()).toContain('bg-card')
    expect(button.attributes('data-testid')).toBe('my-select')
  })

  it('clicking an option emits update:modelValue with the string value', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    await w.get('button').trigger('click')
    await optionEls()[2].dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['c'])
  })

  it('coerces the emitted value to a number when modelValue was a number', async () => {
    const numOptions = [{ value: 1, label: 'One' }, { value: 2, label: 'Two' }]
    const w = mountSelect({ modelValue: 1, options: numOptions })
    await w.get('button').trigger('click')
    await optionEls()[1].dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    const emitted = w.emitted('update:modelValue')?.[0]
    expect(emitted?.[0]).toBe(2)
    expect(typeof emitted?.[0]).toBe('number')
  })

  it('clicking a disabled option emits nothing', async () => {
    const w = mountSelect({ modelValue: 'a', options: optionsWithDisabled })
    await w.get('button').trigger('click')
    await optionEls()[1].dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:modelValue')).toBeUndefined()
  })

  it('keyboard navigation skips disabled options', async () => {
    const w = mountSelect({ modelValue: 'a', options: optionsWithDisabled })
    const button = w.get('button')
    await button.trigger('keydown', { key: 'ArrowDown' }) // opens, active = selected ('a', index 0)
    await button.trigger('keydown', { key: 'ArrowDown' }) // skips disabled 'b' (index 1) -> 'c' (index 2)
    expect(button.attributes('aria-activedescendant')).toBe(optionEls()[2].id)
  })

  it('arrowDown opens the panel', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    expect(panel()).toBeNull()
    await w.get('button').trigger('keydown', { key: 'ArrowDown' })
    expect(panel()).not.toBeNull()
  })

  it('arrowDown/ArrowUp move the active option', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    const button = w.get('button')
    await button.trigger('keydown', { key: 'ArrowDown' }) // open, active = 'a' (index 0)
    await button.trigger('keydown', { key: 'ArrowDown' }) // -> 'b' (index 1)
    expect(button.attributes('aria-activedescendant')).toBe(optionEls()[1].id)
    await button.trigger('keydown', { key: 'ArrowUp' }) // -> 'a' (index 0)
    expect(button.attributes('aria-activedescendant')).toBe(optionEls()[0].id)
  })

  it('enter selects the active option and closes the panel', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    const button = w.get('button')
    await button.trigger('keydown', { key: 'ArrowDown' }) // open, active = 'a'
    await button.trigger('keydown', { key: 'ArrowDown' }) // active = 'b'
    await button.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('update:modelValue')?.[0]).toEqual(['b'])
    expect(panel()).toBeNull()
  })

  it('escape closes without emitting and returns focus to the trigger', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    const button = w.get('button')
    ;(button.element as HTMLButtonElement).focus()
    await button.trigger('keydown', { key: 'ArrowDown' })
    await button.trigger('keydown', { key: 'ArrowDown' })
    await button.trigger('keydown', { key: 'Escape' })
    expect(panel()).toBeNull()
    expect(w.emitted('update:modelValue')).toBeUndefined()
    expect(document.activeElement).toBe(button.element)
  })

  it('tab closes the panel', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    const button = w.get('button')
    await button.trigger('keydown', { key: 'ArrowDown' })
    expect(panel()).not.toBeNull()
    await button.trigger('keydown', { key: 'Tab' })
    expect(panel()).toBeNull()
  })

  it('mousedown outside the trigger and panel closes it', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    await w.get('button').trigger('keydown', { key: 'ArrowDown' })
    expect(panel()).not.toBeNull()
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await w.vm.$nextTick()
    expect(panel()).toBeNull()
  })

  it('type-ahead jumps to the first enabled option whose label starts with the typed prefix', async () => {
    const fruitOptions = [
      { value: 'a', label: 'Apple' },
      { value: 'b', label: 'Banana' },
      { value: 'c', label: 'Cherry' },
    ]
    const w = mountSelect({ modelValue: 'a', options: fruitOptions })
    const button = w.get('button')
    await button.trigger('keydown', { key: 'ArrowDown' }) // open
    await button.trigger('keydown', { key: 'c' })
    expect(button.attributes('aria-activedescendant')).toBe(optionEls()[2].id)
  })

  it('has combobox/listbox ARIA wiring: role, aria-expanded, aria-selected, aria-activedescendant', async () => {
    const w = mountSelect({ modelValue: 'b', options })
    const button = w.get('button')
    expect(button.attributes('role')).toBe('combobox')
    expect(button.attributes('aria-haspopup')).toBe('listbox')
    expect(button.attributes('aria-expanded')).toBe('false')

    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(panel()?.getAttribute('role')).toBe('listbox')

    const opts = optionEls()
    expect(opts[1].getAttribute('aria-selected')).toBe('true')
    expect(opts[0].getAttribute('aria-selected')).toBe('false')
    expect(button.attributes('aria-activedescendant')).toBe(opts[1].id)
    expect(button.attributes('aria-controls')).toBe(panel()?.id)
  })

  it('disabled options carry aria-disabled', async () => {
    const w = mountSelect({ modelValue: 'a', options: optionsWithDisabled })
    await w.get('button').trigger('click')
    expect(optionEls()[1].getAttribute('aria-disabled')).toBe('true')
  })

  it('has no axe violations on the closed trigger', async () => {
    const w = mountSelect({ modelValue: 'a', options, ariaLabel: 'Choose option' })
    expect(await axe(w.get('button').element as HTMLElement)).toHaveNoViolations()
  })

  it('has no axe violations on the open panel', async () => {
    const w = mountSelect({ modelValue: 'a', options, ariaLabel: 'Choose option' })
    await w.get('button').trigger('click')
    expect(await axe(panel() as HTMLElement)).toHaveNoViolations()
  })

  it('escape with the panel open does not let the event reach a parent handler', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    const button = w.get('button')
    await button.trigger('keydown', { key: 'ArrowDown' }) // opens the panel
    const parentHandler = vi.fn()
    document.addEventListener('keydown', parentHandler)
    try {
      await button.trigger('keydown', { key: 'Escape' })
      expect(parentHandler).not.toHaveBeenCalled()
    }
    finally {
      document.removeEventListener('keydown', parentHandler)
    }
  })

  it('escape with the panel closed lets the event reach a parent handler', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    const button = w.get('button')
    const parentHandler = vi.fn()
    document.addEventListener('keydown', parentHandler)
    try {
      await button.trigger('keydown', { key: 'Escape' }) // panel is already closed
      expect(parentHandler).toHaveBeenCalledTimes(1)
    }
    finally {
      document.removeEventListener('keydown', parentHandler)
    }
  })

  it('selecting the already-selected option emits nothing', async () => {
    const w = mountSelect({ modelValue: 'b', options })
    await w.get('button').trigger('click')
    await optionEls()[1].dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.emitted('update:modelValue')).toBeUndefined()
    expect(panel()).toBeNull()
  })

  it('opening focuses the trigger', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    const button = w.get('button').element as HTMLButtonElement
    expect(document.activeElement).not.toBe(button)
    await w.get('button').trigger('click')
    expect(document.activeElement).toBe(button)
  })

  it('home/End move the active option', async () => {
    const w = mountSelect({ modelValue: 'a', options })
    const button = w.get('button')
    await button.trigger('keydown', { key: 'ArrowDown' }) // open, active = 'a' (index 0)
    await button.trigger('keydown', { key: 'End' })
    expect(button.attributes('aria-activedescendant')).toBe(optionEls()[2].id)
    await button.trigger('keydown', { key: 'Home' })
    expect(button.attributes('aria-activedescendant')).toBe(optionEls()[0].id)
  })

  it('an options array where every option is disabled does not hang or throw', async () => {
    const allDisabled = [
      { value: 'a', label: 'Option A', disabled: true },
      { value: 'b', label: 'Option B', disabled: true },
    ]
    const w = mountSelect({ modelValue: 'zzz', options: allDisabled })
    const button = w.get('button')
    await expect(button.trigger('keydown', { key: 'ArrowDown' })).resolves.not.toThrow()
    await expect(button.trigger('keydown', { key: 'ArrowDown' })).resolves.not.toThrow()
    expect(panel()).not.toBeNull()
  })

  it('an empty options array opens without throwing', async () => {
    const w = mountSelect({ modelValue: 'a', options: [] })
    const button = w.get('button')
    await expect(button.trigger('keydown', { key: 'ArrowDown' })).resolves.not.toThrow()
    expect(panel()).not.toBeNull()
  })

  it('type-ahead buffer resets after its timeout', async () => {
    vi.useFakeTimers()
    try {
      const fruitOptions = [
        { value: 'a', label: 'Apple' },
        { value: 'b', label: 'Banana' },
        { value: 'c', label: 'Cherry' },
      ]
      const w = mountSelect({ modelValue: 'a', options: fruitOptions })
      const button = w.get('button')
      await button.trigger('keydown', { key: 'ArrowDown' }) // open
      await button.trigger('keydown', { key: 'b' }) // buffer 'b' -> Banana
      expect(button.attributes('aria-activedescendant')).toBe(optionEls()[1].id)

      vi.advanceTimersByTime(500) // buffer times out and resets

      await button.trigger('keydown', { key: 'a' }) // fresh buffer 'a' -> Apple
      expect(button.attributes('aria-activedescendant')).toBe(optionEls()[0].id)
    }
    finally {
      vi.useRealTimers()
    }
  })
})
