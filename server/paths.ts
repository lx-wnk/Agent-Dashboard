import { homedir } from 'node:os'
import { join } from 'node:path'

/** Base directory for Claude Code project session files */
export const CLAUDE_PROJECTS_DIR = join(homedir(), '.claude', 'projects')

/** Directory containing aggregated session metadata JSON files */
export const SESSION_META_DIR = join(homedir(), '.claude', 'usage-data', 'session-meta')

/** Directory for dashboard channel discovery files */
export const DISCOVERY_DIR = join(homedir(), '.claude', 'dashboard-channel')

/** Shared regex for splitting on whitespace (used by process/system parsers) */
export const WHITESPACE_RE = /\s+/
