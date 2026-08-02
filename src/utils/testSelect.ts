import type { VueWrapper } from '@vue/test-utils'
import type { SelectOption } from '@/components/ui/selectOption'
import { flushPromises } from '@vue/test-utils'

/**
 * Shared helpers for driving AppSelect in component tests. AppSelect is a
 * custom listbox, not a native <select> — its panel teleports to <body>
 * while open, so options are read through the trigger + the teleported
 * [role="option"] elements instead of wrapper.findAll('option') /
 * select.setValue(), which only work against native <select> internals.
 */

interface SelectTrigger {
  trigger: (event: string) => Promise<unknown>
  attributes: (key: string) => string | undefined
}

/** Opens an AppSelect panel via a @vue/test-utils DOMWrapper and returns it. */
export async function openListbox(select: SelectTrigger): Promise<HTMLElement> {
  await select.trigger('click')
  return document.getElementById(select.attributes('aria-controls')!)!
}

/** Opens an AppSelect panel via a raw DOM trigger element and returns it. */
export async function openListboxDom(trigger: Element): Promise<HTMLElement> {
  trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
  return document.getElementById(trigger.getAttribute('aria-controls')!)!
}

/**
 * Finds an option row by its visible label. Matches the label's own span
 * rather than the row's full textContent, because the selected option also
 * renders a "✓" span — its textContent would otherwise be "label✓".
 */
export function optionByLabel(panel: HTMLElement, label: string): HTMLElement {
  const match = Array.from(panel.querySelectorAll('[role="option"]'))
    .find(el => el.querySelector('span')?.textContent?.trim() === label)
  if (!match)
    throw new Error(`No option with label "${label}" found`)
  return match as HTMLElement
}

/** Opens an AppSelect via a raw DOM trigger and clicks the option with the given label. */
export async function selectByLabel(trigger: Element, label: string): Promise<void> {
  const panel = await openListboxDom(trigger)
  optionByLabel(panel, label).dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
}

interface PropsReader { props: (key: string) => unknown }

/**
 * Finds a mounted AppSelect by its `id` prop and returns its typed
 * `options`. AppSelect is generic, so Vue Test Utils cannot infer its
 * instance type: findAllComponents yields WrapperLike, which exposes no
 * props() at all. Generic SFCs also can't be passed to findAllComponents as
 * a value, so the lookup matches by component name ('AppSelect') instead of
 * by import — this is the one place that costs a cast, buying its removal
 * from every call site.
 */
export function selectOptionsById<T extends string | number = string>(wrapper: VueWrapper, id: string): SelectOption<T>[] {
  const match = wrapper.findAllComponents({ name: 'AppSelect' })
    .find(c => (c as unknown as PropsReader).props('id') === id)
  if (!match)
    throw new Error(`No AppSelect with id "${id}" found`)
  return (match as unknown as PropsReader).props('options') as SelectOption<T>[]
}
