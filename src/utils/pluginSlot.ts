// src/utils/pluginSlot.ts
// Generic, app-wide frontend plugin-slot contract. Core knows only that a plugin
// may mount UI into a named slot and receives the per-slot context declared below.

import type { Agent, PipelineTask } from '../types'

/** Refinement textarea slot (voice plugins): drive input text + busy state. */
export interface RefinementInputContext {
  /** Insert text into the host input (e.g. the refinement textarea). */
  insertText: (text: string) => void
  /** Reflect a busy state (recording / transcribing) in the host UI. */
  setBusy: (busy: boolean) => void
}

/** Slots that render alongside a single task (modal footer, kanban badge). */
export interface TaskSlotContext {
  task: PipelineTask
}

/** Slots that render alongside a single live agent (agent modal footer). */
export interface AgentSlotContext {
  agent: Agent
}

/** Settings panel slot — no entity context. */
export type SettingsSlotContext = Record<string, never>

/**
 * Central registry of slot name → context shape. Single source of truth for which
 * slots exist and what each addon receives. Adding a slot = adding one entry here.
 */
export interface SlotContracts {
  'refinement-input-addon': RefinementInputContext
  'task-modal-footer': TaskSlotContext
  'agent-modal-footer': AgentSlotContext
  'kanban-card-badge': TaskSlotContext
  'settings-panel': SettingsSlotContext
}

export const SLOT_NAMES = [
  'refinement-input-addon',
  'task-modal-footer',
  'agent-modal-footer',
  'kanban-card-badge',
  'settings-panel',
] as const

export type SlotName = keyof SlotContracts

/** Back-compat alias: the original single-slot context was the refinement one. */
export type SlotContext = RefinementInputContext

/** FE mirror of the server `ui_extension` capability (server SSOT: plugin/types.go). */
export const PLUGIN_UI_CAPABILITY = 'ui_extension'

export type UnmountFn = () => void

/** A composed lower-priority chain an `extend` addon may mount/wrap. */
export interface SlotParent {
  mount: (el: HTMLElement) => UnmountFn
}

/**
 * Author-facing addon type: precise per-slot context. A plugin targeting one slot
 * declares `SlotAddon<'task-modal-footer'>` and gets a typed `ctx`.
 */
export interface SlotAddon<S extends SlotName = SlotName> {
  /**
   * Which named slot this addon targets. Optional: legacy `addon.js` modules that
   * predate the UI manifest may omit it (the loader infers the slot from context).
   */
  slot?: S
  /** Higher renders outer/first. Default 0. */
  priority?: number
  /** 'override' = own the slot exclusively; 'extend' = wrap the parent chain. Undefined = sibling. */
  mode?: 'override' | 'extend'
  /** Mount the addon UI into `el`; return a teardown fn. */
  mount: (el: HTMLElement, ctx: SlotContracts[S], parent?: SlotParent | null) => UnmountFn
}

/**
 * Loader/host boundary addon: the ctx is type-erased because the dynamically imported
 * plugin module's slot is only known at runtime — the framework cannot statically prove
 * a given module matches a given slot, so it trusts the manifest / `mod.default.slot`.
 */
export interface LoadedAddon {
  slot?: string
  priority?: number
  mode?: 'override' | 'extend'
  mount: (el: HTMLElement, ctx: any, parent?: SlotParent | null) => UnmountFn
}

export interface SlotAddonModule {
  default: LoadedAddon
}
