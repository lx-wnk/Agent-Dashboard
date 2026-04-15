import type { NotificationAdapter, NotificationPayload } from '../types.js'

/**
 * Browser adapter: pushes a notification event to all connected SSE clients.
 * The frontend uses the Web Notifications API to display it.
 *
 * The dispatcher injects the broadcast function at runtime via setSseBroadcaster.
 */

type Broadcaster = (payload: NotificationPayload) => void

let broadcaster: Broadcaster | null = null

export function setSseBroadcaster(fn: Broadcaster): void {
  broadcaster = fn
}

export const browserAdapter: NotificationAdapter = {
  channel: 'browser',

  isConfigured() {
    return broadcaster !== null
  },

  async send(payload: NotificationPayload): Promise<void> {
    if (!broadcaster)
      throw new Error('browser adapter not configured (no SSE broadcaster set)')
    broadcaster(payload)
  },
}
