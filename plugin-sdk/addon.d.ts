// agent-dashboard plugin UI-addon types.
// MIRROR of src/utils/pluginSlot.ts (the source of truth). Keep in parity by hand:
// when the host's slot contract changes, update this file too.

export interface RefinementInputContext {
  insertText: (text: string) => void
  setBusy: (busy: boolean) => void
}
export interface TaskSlotContext { task: unknown }
export interface AgentSlotContext { agent: unknown }
export type SettingsSlotContext = Record<string, never>

export interface SlotContracts {
  'refinement-input-addon': RefinementInputContext
  'task-modal-footer': TaskSlotContext
  'agent-modal-footer': AgentSlotContext
  'kanban-card-badge': TaskSlotContext
  'settings-panel': SettingsSlotContext
}

export type SlotName = keyof SlotContracts
export type UnmountFn = () => void

export interface SlotParent {
  mount: (el: HTMLElement) => UnmountFn
}

export interface SlotAddon<S extends SlotName = SlotName> {
  slot?: S
  /** Higher renders outer/first. Default 0. */
  priority?: number
  /** 'override' = own the slot exclusively; 'extend' = wrap the parent chain; omit = sibling. */
  mode?: 'override' | 'extend'
  mount: (el: HTMLElement, ctx: SlotContracts[S], parent?: SlotParent | null) => UnmountFn
}
