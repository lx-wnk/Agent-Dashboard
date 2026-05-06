import type { PipelineStage } from '../src/types.js'
import process from 'node:process'

// Narrower than the DB CHECK constraint — old stages (pruefung, refinement,
// planning, approval1, umsetzungskonzept, approval2) are excluded intentionally.
// The DB CHECK stays broad so legacy rows survive migrations safely.
export const VALID_STAGES = new Set<PipelineStage>([
  'konzept',
  'backlog',
  'umsetzung',
  'selbstreview',
  'finalisierung',
  'done',
  'on_hold',
  'cancelled',
])

export const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/
export const SLUG_PATTERN_MESSAGE = 'slug must match [a-z0-9][a-z0-9-]{0,63}'

export const SYSTEM_PROMPT_MAX_CHARS = 10_000

export const DEPENDENCY_REQUIRED_STAGES = ['done', 'cancelled'] as const
export const DEPENDENCY_CANCEL_ACTIONS = ['cancel', 'start', 'on_hold'] as const

export const UUID_RE = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i

// ─── Network / endpoint constants ──────────────────────────────────
//
// `DASHBOARD_PORT` env var overrides the dashboard's bind port; the helper
// `resolveDashboardPort()` wraps the lookup so callers don't repeat the
// `process.env.DASHBOARD_PORT ?? DEFAULT_DASHBOARD_PORT` pattern. Hard-
// coded literals (13120, '13120') previously appeared in 5+ places — every
// drift between them caused subtle bugs (e.g. self-skip in remote-aggregator
// using the wrong default while stage spawns used another).

export const DEFAULT_DASHBOARD_PORT = 13120

export const LOOPBACK_HOST = '127.0.0.1'

/**
 * Set of hostnames that resolve to "this machine" — used by remote-
 *  aggregation logic to skip self-registered remotes. Must match what
 *  the dashboard actually binds to (LOOPBACK_HOST is the secure default).
 */
export const SELF_HOSTNAMES = new Set([LOOPBACK_HOST, 'localhost', '[::1]', '0.0.0.0'])

/**
 * Default fetch/abort timeout for dashboard-to-dashboard remote calls
 *  and similarly-shaped network probes (webhook adapter, channel reply).
 */
export const DEFAULT_REMOTE_TIMEOUT_MS = 5000

/**
 * Mount path of the dashboard's MCP endpoint; injected into spawned stage
 *  agents via `DASHBOARD_MCP_URL` so the channel bridge can call back.
 */
export const MCP_API_PATH = '/api/mcp'

/**
 * Resolve the dashboard's HTTP port from env (`DASHBOARD_PORT`) with a
 *  consistent default. Returns the parsed numeric port or the default
 *  on missing/invalid input — never throws. Single source of truth so a
 *  malformed env var doesn't silently produce two different ports across
 *  modules.
 */
export function resolveDashboardPort(): number {
  const raw = process.env.DASHBOARD_PORT
  if (raw === undefined)
    return DEFAULT_DASHBOARD_PORT
  const parsed = Number.parseInt(raw, 10)
  if (!Number.isFinite(parsed) || parsed <= 0 || parsed > 65535)
    return DEFAULT_DASHBOARD_PORT
  return parsed
}
