import type { Database } from './client.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'

export interface PermissionPresetEntry {
  tool: string
  pattern: string | null
}

interface PermissionPresetRow {
  tool: string
  pattern: string | null
}

/**
 * Idempotently records a (user_id, project_cwd, tool, pattern) preset entry.
 * Re-confirming the same combination is a no-op thanks to the UNIQUE
 * constraint + INSERT OR IGNORE. `user_id = NULL` means "shared across all
 * users in this project".
 */
export function upsertPreset(
  userId: string | null,
  projectCwd: string,
  tool: string,
  pattern: string | null,
  db: Database = getDb(),
): void {
  const id = randomUUID()
  db.prepare(`
    INSERT OR IGNORE INTO permission_presets (id, user_id, project_cwd, tool, pattern)
    VALUES (@id, @user_id, @project_cwd, @tool, @pattern)
  `).run({
    id,
    user_id: userId,
    project_cwd: projectCwd,
    tool,
    pattern,
  })
}

/**
 * Lists preset entries applicable to (userId, projectCwd). Returns both the
 * caller's own presets and any project-wide (user_id IS NULL) presets so a
 * shared baseline can be applied to every user's tasks.
 */
export function listPresets(
  userId: string | null,
  projectCwd: string,
  db: Database = getDb(),
): PermissionPresetEntry[] {
  const rows = db
    .prepare(`
      SELECT tool, pattern FROM permission_presets
      WHERE project_cwd = @project_cwd
        AND (user_id = @user_id OR user_id IS NULL)
    `)
    .all({ user_id: userId, project_cwd: projectCwd }) as PermissionPresetRow[]
  return rows.map(r => ({ tool: r.tool, pattern: r.pattern }))
}

export interface PresetProjectSummary {
  cwd: string
  count: number
}

/**
 * Returns one row per project_cwd with the total number of preset entries
 * applicable to (userId). Includes the caller's own presets and any
 * project-wide (user_id IS NULL) presets, mirroring `listPresets`.
 */
export function listPresetProjectSummaries(
  userId: string | null,
  db: Database = getDb(),
): PresetProjectSummary[] {
  const rows = db
    .prepare(`
      SELECT project_cwd as cwd, COUNT(*) as count
      FROM permission_presets
      WHERE (user_id = @user_id OR user_id IS NULL)
      GROUP BY project_cwd
      ORDER BY project_cwd
    `)
    .all({ user_id: userId }) as { cwd: string, count: number }[]
  return rows
}

/**
 * Deletes all preset entries for a (userId, projectCwd) combination.
 * `userId = null` deletes only project-wide (shared) entries — it does NOT
 * cascade into per-user rows.
 */
export function deletePresetsForProject(
  userId: string | null,
  projectCwd: string,
  db: Database = getDb(),
): void {
  if (userId === null) {
    db.prepare(`
      DELETE FROM permission_presets
      WHERE user_id IS NULL AND project_cwd = @project_cwd
    `).run({ project_cwd: projectCwd })
  }
  else {
    db.prepare(`
      DELETE FROM permission_presets
      WHERE user_id = @user_id AND project_cwd = @project_cwd
    `).run({ user_id: userId, project_cwd: projectCwd })
  }
}
