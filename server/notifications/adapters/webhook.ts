import type { NotificationAdapter, NotificationPayload } from '../types.js'
import { getConfig } from '../../db/notificationConfigRepo.js'

/**
 * Webhook adapter for Discord/Slack/generic POST endpoints.
 * Required config: webhook_url
 * Optional: webhook_format ("discord" | "slack" | "generic")
 */
export const webhookAdapter: NotificationAdapter = {
  channel: 'webhook',

  isConfigured() {
    return !!getConfig('webhook_url')
  },

  async send(payload: NotificationPayload): Promise<void> {
    const url = getConfig('webhook_url')
    if (!url)
      throw new Error('webhook adapter missing webhook_url')

    if (!isSafeWebhookUrl(url))
      throw new Error(`webhook_url blocked by SSRF guard: ${url}`)

    const format = getConfig('webhook_format') || 'generic'
    const body = buildBody(format, payload)

    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })

    if (!res.ok) {
      const text = await res.text().catch(() => '')
      throw new Error(`webhook returned ${res.status}: ${text.slice(0, 200)}`)
    }
  },
}

function isSafeWebhookUrl(raw: string): boolean {
  try {
    const u = new URL(raw)
    if (u.protocol !== 'http:' && u.protocol !== 'https:')
      return false
    const host = u.hostname.toLowerCase()
    // Block loopback and link-local — same rules as remoteRoutes
    if (/^127\.\d+\.\d+\.\d+$/.test(host) || /^169\.254\.\d+\.\d+$/.test(host))
      return false
    if (host === 'localhost' || host === '::1' || host === '[::1]')
      return false
    return true
  }
  catch {
    return false
  }
}

function buildBody(format: string, payload: NotificationPayload): Record<string, unknown> {
  const summary = `**[${payload.eventType}]** ${payload.title}\n${payload.body}`
  const link = payload.url ? `\n<${payload.url}>` : ''

  switch (format) {
    case 'discord':
      return { content: `${summary}${link}` }
    case 'slack':
      return {
        text: `${summary}${link}`,
        attachments: [
          {
            color: payload.severity === 'error' ? '#ff0000' : payload.severity === 'warning' ? '#ffaa00' : '#0099ff',
            fields: [
              { title: 'Task', value: `${payload.taskSlug} (${payload.taskId})`, short: true },
              { title: 'Event', value: payload.eventType, short: true },
            ],
          },
        ],
      }
    case 'generic':
    default:
      return {
        eventType: payload.eventType,
        title: payload.title,
        body: payload.body,
        taskId: payload.taskId,
        taskSlug: payload.taskSlug,
        url: payload.url,
        severity: payload.severity ?? 'info',
        timestamp: new Date().toISOString(),
      }
  }
}
