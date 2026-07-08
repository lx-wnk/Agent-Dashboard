import type { UseTerminalSocket, UseTerminalSocketOptions } from '../composables/useTerminalSocket'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AgentTerminal from './AgentTerminal.vue'

const mocks = vi.hoisted(() => ({
  openMock: vi.fn(),
  writeMock: vi.fn(),
  loadAddonMock: vi.fn(),
  disposeMock: vi.fn(),
  fitMock: vi.fn(),
  sendMock: vi.fn(),
  closeMock: vi.fn(),
  useTerminalSocketMock: vi.fn(),
  termOnData: { current: null as ((data: string) => void) | null },
  socketOnData: { current: null as ((bytes: Uint8Array) => void) | null },
}))

vi.mock('@xterm/xterm', () => {
  class FakeTerminal {
    cols = 80
    rows = 24
    open = mocks.openMock
    write = mocks.writeMock
    loadAddon = mocks.loadAddonMock
    dispose = mocks.disposeMock
    onData(cb: (data: string) => void): void {
      mocks.termOnData.current = cb
    }
  }
  return { Terminal: FakeTerminal }
})

vi.mock('@xterm/addon-fit', () => {
  class FakeFitAddon {
    fit = mocks.fitMock
  }
  return { FitAddon: FakeFitAddon }
})

vi.mock('../composables/useTerminalSocket', () => ({
  useTerminalSocket: (pid: number, opts: UseTerminalSocketOptions): UseTerminalSocket => {
    mocks.useTerminalSocketMock(pid, opts)
    mocks.socketOnData.current = opts.onData
    return {
      send: mocks.sendMock,
      resize: vi.fn(),
      status: { value: 'open' } as UseTerminalSocket['status'],
      close: mocks.closeMock,
    }
  },
}))

describe('agentTerminal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.termOnData.current = null
    mocks.socketOnData.current = null
  })

  it('creates a terminal and opens it in the container', () => {
    const wrapper = mount(AgentTerminal, { props: { pid: 1234 } })

    expect(mocks.openMock).toHaveBeenCalledTimes(1)
    expect(mocks.openMock.mock.calls[0][0]).toBeInstanceOf(HTMLElement)
    expect(mocks.useTerminalSocketMock).toHaveBeenCalledWith(1234, expect.any(Object))

    wrapper.unmount()
  })

  it('writes incoming socket bytes to the terminal', () => {
    mount(AgentTerminal, { props: { pid: 1234 } })

    const bytes = new Uint8Array([104, 105])
    mocks.socketOnData.current?.(bytes)

    expect(mocks.writeMock).toHaveBeenCalledWith(bytes)
  })

  it('sends encoded terminal input to the socket', () => {
    mount(AgentTerminal, { props: { pid: 1234 } })

    mocks.termOnData.current?.('x')

    expect(mocks.sendMock).toHaveBeenCalledWith(new TextEncoder().encode('x'))
  })

  it('disposes the terminal and closes the socket on unmount', () => {
    const wrapper = mount(AgentTerminal, { props: { pid: 1234 } })

    wrapper.unmount()

    expect(mocks.disposeMock).toHaveBeenCalledTimes(1)
    expect(mocks.closeMock).toHaveBeenCalledTimes(1)
  })
})
