import type { NotificationAdapter, NotificationPayload } from '../types.js'
import nodemailer from 'nodemailer'
import { getConfig } from '../../db/notificationConfigRepo.js'

/**
 * Email adapter using nodemailer with SMTP.
 * Required config keys:
 *   smtp_host, smtp_port, smtp_user, smtp_pass, smtp_from, smtp_to
 * Optional:
 *   smtp_secure ("true"/"false")
 */
export const emailAdapter: NotificationAdapter = {
  channel: 'email',

  isConfigured() {
    return (
      !!getConfig('smtp_host')
      && !!getConfig('smtp_port')
      && !!getConfig('smtp_from')
      && !!getConfig('smtp_to')
    )
  },

  async send(payload: NotificationPayload): Promise<void> {
    const host = getConfig('smtp_host')
    const port = Number(getConfig('smtp_port'))
    const user = getConfig('smtp_user')
    const pass = getConfig('smtp_pass')
    const from = getConfig('smtp_from')
    const to = getConfig('smtp_to')
    const secure = getConfig('smtp_secure') === 'true'

    if (!host || !port || !from || !to)
      throw new Error('email adapter missing required config')

    const transport = nodemailer.createTransport({
      host,
      port,
      secure,
      auth: user && pass ? { user, pass } : undefined,
    })

    const subject = `[${payload.eventType}] ${payload.title}`
    const text = [
      payload.body,
      '',
      `Task: ${payload.taskSlug} (${payload.taskId})`,
      payload.url ? `URL: ${payload.url}` : '',
    ].filter(Boolean).join('\n')

    await transport.sendMail({ from, to, subject, text })
  },
}
