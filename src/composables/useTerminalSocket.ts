import type { Ref } from 'vue'
import { getCurrentScope, onScopeDispose, ref } from 'vue'
import { SSE_RETRY_DELAY_MS } from '../utils/sse'

export type TerminalSocketStatus = 'connecting' | 'open' | 'closed'

export interface UseTerminalSocketOptions {
  onData: (bytes: Uint8Array) => void
}

export interface UseTerminalSocket {
  send: (bytes: Uint8Array) => void
  resize: (cols: number, rows: number) => void
  status: Ref<TerminalSocketStatus>
  close: () => void
}

function buildTerminalUrl(pid: number): string {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${location.host}/api/agents/${pid}/terminal`
}

export function useTerminalSocket(pid: number, opts: UseTerminalSocketOptions): UseTerminalSocket {
  const status = ref<TerminalSocketStatus>('connecting')
  let ws: WebSocket | null = null
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let closed = false

  function clearReconnectTimer(): void {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
  }

  function connect(): void {
    status.value = 'connecting'
    const socket = new WebSocket(buildTerminalUrl(pid))
    socket.binaryType = 'arraybuffer'

    socket.onopen = () => {
      status.value = 'open'
    }

    socket.onmessage = (e) => {
      if (e.data instanceof ArrayBuffer)
        opts.onData(new Uint8Array(e.data))
    }

    socket.onclose = () => {
      status.value = 'closed'
      if (closed)
        return
      clearReconnectTimer()
      reconnectTimer = setTimeout(connect, SSE_RETRY_DELAY_MS)
    }

    ws = socket
  }

  connect()

  function send(bytes: Uint8Array): void {
    if (ws?.readyState === WebSocket.OPEN)
      ws.send(bytes)
  }

  function resize(cols: number, rows: number): void {
    if (ws?.readyState === WebSocket.OPEN)
      ws.send(JSON.stringify({ resize: { cols, rows } }))
  }

  function close(): void {
    closed = true
    clearReconnectTimer()
    ws?.close()
  }

  if (getCurrentScope())
    onScopeDispose(close)

  return { send, resize, status, close }
}
