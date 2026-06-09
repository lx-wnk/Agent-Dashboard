// src/utils/pluginSlot.ts
// Generic frontend plugin-slot contract. Voice-agnostic: core knows only that a
// plugin may mount UI into a named slot and gets these two callbacks.

export interface SlotContext {
  /** Insert text into the host input (e.g. the refinement textarea). */
  insertText: (text: string) => void
  /** Reflect a busy state (recording / transcribing) in the host UI. */
  setBusy: (busy: boolean) => void
}

export type UnmountFn = () => void

export interface SlotAddon {
  /** Which named slot this addon targets, e.g. "refinement-input-addon". */
  slot: string
  /** Mount the addon UI into `el`; return a teardown fn. */
  mount: (el: HTMLElement, ctx: SlotContext) => UnmountFn
}

export interface SlotAddonModule {
  default: SlotAddon
}
