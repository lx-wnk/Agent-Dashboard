import { realpathSync } from 'node:fs'
import { join } from 'node:path'

export const CHANNEL_DIR = join(import.meta.dirname, '..', 'channel')
export const CHANNEL_SCRIPT = join(CHANNEL_DIR, 'dashboard-channel.ts')
export const CHANNEL_TSX = join(CHANNEL_DIR, 'node_modules', '.bin', 'tsx')

export function buildDashboardChannelMcpConfig(): string {
  return JSON.stringify({
    mcpServers: {
      'dashboard-channel': {
        command: realpathSync(CHANNEL_TSX),
        args: [realpathSync(CHANNEL_SCRIPT)],
      },
    },
  })
}
