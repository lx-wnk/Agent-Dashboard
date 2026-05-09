import type { PushSubscriptionObject } from '../notifications/adapters/webpush.js'
import { Router } from 'express'
import webPush from 'web-push'
import { getConfig, setConfig } from '../db/notificationConfigRepo.js'
import { registerSubscription } from '../notifications/adapters/webpush.js'

export function createWebPushRouter(): ReturnType<typeof Router> {
  const router = Router()

  // Generate and persist VAPID keys (idempotent)
  router.post('/settings/webpush/vapid', (req, res) => {
    const existing = getConfig('vapid_public_key')
    if (existing) {
      res.json({ alreadyGenerated: true, publicKey: existing })
      return
    }
    const { publicKey, privateKey } = webPush.generateVAPIDKeys()
    setConfig('vapid_public_key', publicKey)
    setConfig('vapid_private_key', privateKey)
    setConfig('vapid_subject', req.body?.subject ?? 'mailto:admin@localhost')
    res.json({ publicKey })
  })

  // Return the public VAPID key (needed by the browser to subscribe)
  router.get('/settings/webpush/vapid', (_req, res) => {
    const publicKey = getConfig('vapid_public_key')
    if (!publicKey) {
      res.status(404).json({ error: 'VAPID keys not yet generated' })
      return
    }
    res.json({ publicKey })
  })

  // Store a browser push subscription
  router.post('/settings/webpush/subscribe', (req, res) => {
    const sub = req.body as PushSubscriptionObject
    if (!sub?.endpoint || !sub?.keys?.p256dh || !sub?.keys?.auth) {
      res.status(400).json({ error: 'Invalid subscription object' })
      return
    }
    registerSubscription(sub)
    res.json({ ok: true })
  })

  return router
}
