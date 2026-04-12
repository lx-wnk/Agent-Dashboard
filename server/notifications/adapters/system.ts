import type { NotificationAdapter, NotificationPayload } from '../types.js'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { IS_LINUX } from '../../platform.js'

const execFileAsync = promisify(execFile)

/**
 * System notification adapter: uses native OS notifications.
 * macOS: osascript -e 'display notification ...'
 * Linux: notify-send
 * Windows: not supported
 */
export const systemAdapter: NotificationAdapter = {
  channel: 'system',

  isConfigured() {
    return true // always available on supported platforms
  },

  async send(payload: NotificationPayload): Promise<void> {
    const title = `[${payload.eventType}] ${payload.title}`
    const body = payload.body

    if (IS_LINUX) {
      await execFileAsync('notify-send', [title, body])
      return
    }

    // macOS — use osascript. Strings must be escaped for AppleScript.
    const escapedTitle = title.replace(/"/g, '\\"')
    const escapedBody = body.replace(/"/g, '\\"')
    const script = `display notification "${escapedBody}" with title "${escapedTitle}"`
    await execFileAsync('osascript', ['-e', script])
  },
}
