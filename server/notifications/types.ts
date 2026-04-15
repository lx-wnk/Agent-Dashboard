import type { NotificationChannel, NotificationEventType } from '../../src/types.js'

export interface NotificationPayload {
  eventType: NotificationEventType
  title: string
  body: string
  taskId: string
  taskSlug: string
  url?: string // deep link to task in dashboard
  severity?: 'info' | 'warning' | 'error'
}

export interface NotificationAdapter {
  readonly channel: NotificationChannel
  isConfigured: () => boolean
  send: (payload: NotificationPayload) => Promise<void>
}

export interface DispatchResult {
  channel: NotificationChannel
  ok: boolean
  error?: string
}
