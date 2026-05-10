import type { NotificationAdapter, NotificationPayload } from '../types.js'
import webPush from 'web-push'
import { getConfig } from '../../db/notificationConfigRepo.js'

export interface PushSubscriptionObject {
  endpoint: string
  keys: { auth: string, p256dh: string }
}

// In-memory subscriptions (persist via notification_config in a production hardening pass)
const subscriptions = new Set<string>() // JSON-serialized PushSubscriptionObject

let vapidConfigured = false

export function registerSubscription(sub: PushSubscriptionObject): void {
  subscriptions.add(JSON.stringify(sub))
}

export async function sendWebPush(payload: { body: string, title: string }): Promise<void> {
  const publicKey = getConfig('vapid_public_key')
  const privateKey = getConfig('vapid_private_key')
  const subject = getConfig('vapid_subject') ?? 'mailto:admin@localhost'

  if (!publicKey || !privateKey)
    return // VAPID not configured

  if (!vapidConfigured) {
    webPush.setVapidDetails(subject, publicKey, privateKey)
    vapidConfigured = true
  }

  const jobs = [...subscriptions].map(async (raw) => {
    const sub = JSON.parse(raw) as PushSubscriptionObject
    try {
      await webPush.sendNotification(sub, JSON.stringify(payload))
    }
    catch (err: unknown) {
      const e = err as { statusCode?: number }
      if (e.statusCode === 410) {
        // Subscription expired — remove it
        subscriptions.delete(raw)
      }
    }
  })
  await Promise.allSettled(jobs)
}

export const webpushAdapter: NotificationAdapter = {
  channel: 'webpush',

  isConfigured() {
    return !!(getConfig('vapid_public_key') && getConfig('vapid_private_key'))
  },

  async send(payload: NotificationPayload): Promise<void> {
    await sendWebPush({ title: payload.title, body: payload.body })
  },
}
