import type { NotificationAdapter, NotificationPayload } from '../types.js'
import { DEFAULT_REMOTE_TIMEOUT_MS } from '../../constants.js'
import { getConfig } from '../../db/notificationConfigRepo.js'

const IPV4_RE = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/

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

    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), DEFAULT_REMOTE_TIMEOUT_MS)
    let res: Response
    try {
      res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        signal: controller.signal,
      })
    }
    finally {
      clearTimeout(timeoutId)
    }

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

    // Block by hostname patterns (pre-resolution defense)
    if (host === 'localhost' || host === '::1' || host === '[::1]')
      return false

    // Block if the hostname is a bare IP address that falls in a blocked range
    const ipv4Match = host.match(IPV4_RE)
    if (ipv4Match) {
      const [, a, b] = ipv4Match.map(Number)
      if (
        a === 127 // loopback
        || a === 10 // RFC1918 10.0.0.0/8
        || (a === 172 && b >= 16 && b <= 31) // RFC1918 172.16.0.0/12
        || (a === 192 && b === 168) // RFC1918 192.168.0.0/16
        || (a === 169 && b === 254) // link-local
        || a === 0 // 0.0.0.0
        || a >= 240 // reserved/multicast
      ) {
        return false
      }
    }

    // Block IPv6 private ranges by prefix
    if (host.startsWith('[')) {
      const bare = host.slice(1, -1).toLowerCase()
      if (
        bare === '::1'
        || bare.startsWith('fc') // ULA fc00::/7
        || bare.startsWith('fd')
        || bare.startsWith('fe80') // link-local
      ) {
        return false
      }
    }

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
