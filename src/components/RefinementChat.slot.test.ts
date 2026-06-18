// src/components/RefinementChat.slot.test.ts
import type { SlotAddon } from '../utils/pluginSlot'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RefinementChat from './RefinementChat.vue'

// A fake addon that immediately drives the slot context, simulating a finished
// transcription writing into the refinement textarea.
function capturingAddon(text: string): SlotAddon<'refinement-input-addon'> {
  return {
    slot: 'refinement-input-addon',
    mount: (_el, ctx) => {
      ctx.insertText(text)
      return () => {}
    },
  }
}

// useRefinementChat calls fetch on mount (loadHistory) and then syncStatus.
// Stub them out so the component can mount cleanly in jsdom without network.
beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: async () => [],
    body: null,
  }))
  // EventSource is not used by useRefinementChat (it uses fetch SSE via ReadableStream),
  // but stub it in case jsdom complains about the global being absent.
  vi.stubGlobal('EventSource', vi.fn(() => ({
    close: vi.fn(),
    addEventListener: vi.fn(),
  })))
  // jsdom does not implement scrollTo on elements; stub it to suppress the
  // unhandled rejection from the messages watcher in RefinementChat.
  Element.prototype.scrollTo = vi.fn() as any
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('refinementChat voice slot', () => {
  it('inserts addon text into the refinement textarea', async () => {
    const wrapper = mount(RefinementChat, {
      props: {
        open: true,
        task: { id: 't1', slug: 's', title: 'T', status: 'refinement' } as any,
        slotLoader: async () => [capturingAddon('hello world')],
      },
    })
    await flushPromises()

    const textarea = wrapper.get('textarea[placeholder="Message..."]')
      .element as HTMLTextAreaElement
    expect(textarea.value).toContain('hello world')
  })

  it('disables the textarea while a slot addon reports busy', async () => {
    const busyAddon: SlotAddon<'refinement-input-addon'> = {
      slot: 'refinement-input-addon',
      mount: (_el, ctx) => {
        ctx.setBusy(true)
        return () => {}
      },
    }
    const wrapper = mount(RefinementChat, {
      props: {
        open: true,
        task: { id: 't1', slug: 's', title: 'T', status: 'refinement' } as any,
        slotLoader: async () => [busyAddon],
      },
    })
    await flushPromises()
    const textarea = wrapper.get('textarea[placeholder="Message..."]').element as HTMLTextAreaElement
    expect(textarea.disabled).toBe(true)
  })
})
