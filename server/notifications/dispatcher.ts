import type { NotificationChannel } from '../../src/types.js'
import type { DispatchResult, NotificationAdapter, NotificationPayload } from './types.js'
import { consola } from 'consola'
import { getPreference } from '../db/notificationConfigRepo.js'
import { browserAdapter } from './adapters/browser.js'
import { emailAdapter } from './adapters/email.js'
import { systemAdapter } from './adapters/system.js'
import { webhookAdapter } from './adapters/webhook.js'

export { setSseBroadcaster } from './adapters/browser.js'

const DEFAULT_ADAPTERS: Record<NotificationChannel, NotificationAdapter> = {
  email: emailAdapter,
  webhook: webhookAdapter,
  browser: browserAdapter,
  system: systemAdapter,
}

export interface Dispatcher {
  dispatch: (payload: NotificationPayload) => Promise<DispatchResult[]>
}

/**
 * Build a dispatcher. `adapters` is overridable for tests.
 */
export function createDispatcher(
  adapters: Partial<Record<NotificationChannel, NotificationAdapter>> = DEFAULT_ADAPTERS,
): Dispatcher {
  const merged = { ...DEFAULT_ADAPTERS, ...adapters }

  return {
    async dispatch(payload) {
      const pref = getPreference(payload.eventType)
      if (!pref || !pref.enabled)
        return []

      const results: DispatchResult[] = []
      for (const channel of pref.channels) {
        const adapter = merged[channel]
        if (!adapter) {
          results.push({ channel, ok: false, error: 'adapter not found' })
          continue
        }
        if (!adapter.isConfigured()) {
          results.push({ channel, ok: false, error: 'adapter not configured' })
          continue
        }
        try {
          await adapter.send(payload)
          results.push({ channel, ok: true })
        }
        catch (err) {
          const msg = (err as Error).message
          consola.warn(`[notifications] ${channel} failed: ${msg}`)
          results.push({ channel, ok: false, error: msg })
        }
      }
      return results
    },
  }
}
