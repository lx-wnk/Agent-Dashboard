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
      // notify-send receives args via execFile — no shell, no injection risk.
      await execFileAsync('notify-send', [title, body])
      return
    }

    // macOS — strip control characters, backslashes, and double quotes so
    // user-controlled task titles cannot break out of the AppleScript
    // string literal or inject new AppleScript statements.
    await execFileAsync('osascript', [
      '-e',
      `display notification "${sanitizeForAppleScript(body)}" with title "${sanitizeForAppleScript(title)}"`,
    ])
  },
}

function sanitizeForAppleScript(input: string): string {
  // Remove backslashes (escape char), double quotes (string delimiter),
  // control characters including CR/LF (AppleScript statement separators),
  // and cap length to prevent unbounded growth.
  return input
    // eslint-disable-next-line no-control-regex
    .replace(/[\\"\u0000-\u001F\u007F]/g, ' ')
    .slice(0, 300)
}
