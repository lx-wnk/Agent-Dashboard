import type { Database } from 'better-sqlite3'
import type { NotificationChannel, NotificationEventType, NotificationPreference } from '../../src/types.js'
import type { NotificationPreferenceRow } from './rowMappers.js'
import { getDb } from './client.js'
import { rowToNotificationPreference } from './rowMappers.js'

// --- Notification preferences (event → channels) ---

export function setPreference(
  eventType: NotificationEventType,
  channels: NotificationChannel[],
  enabled: boolean,
  db: Database = getDb(),
): NotificationPreference {
  db.prepare(`
    INSERT INTO notification_preferences (event_type, channels, enabled)
    VALUES (@event_type, @channels, @enabled)
    ON CONFLICT(event_type) DO UPDATE SET
      channels = excluded.channels,
      enabled = excluded.enabled
  `).run({
    event_type: eventType,
    channels: JSON.stringify(channels),
    enabled: enabled ? 1 : 0,
  })
  return getPreference(eventType, db)!
}

export function getPreference(
  eventType: NotificationEventType,
  db: Database = getDb(),
): NotificationPreference | null {
  const row = db
    .prepare('SELECT * FROM notification_preferences WHERE event_type = ?')
    .get(eventType) as NotificationPreferenceRow | undefined
  return row ? rowToNotificationPreference(row) : null
}

export function listPreferences(db: Database = getDb()): NotificationPreference[] {
  const rows = db
    .prepare('SELECT * FROM notification_preferences')
    .all() as NotificationPreferenceRow[]
  return rows.map(rowToNotificationPreference)
}

// --- Notification adapter config (SMTP host, webhook URL, etc.) ---

export function setConfig(key: string, value: string | null, db: Database = getDb()): void {
  db.prepare(`
    INSERT INTO notification_config (key, value) VALUES (?, ?)
    ON CONFLICT(key) DO UPDATE SET value = excluded.value
  `).run(key, value)
}

export function getConfig(key: string, db: Database = getDb()): string | null {
  const row = db
    .prepare('SELECT value FROM notification_config WHERE key = ?')
    .get(key) as { value: string | null } | undefined
  return row?.value ?? null
}

export function getAllConfig(db: Database = getDb()): Record<string, string> {
  const rows = db
    .prepare('SELECT key, value FROM notification_config')
    .all() as { key: string, value: string | null }[]
  const out: Record<string, string> = {}
  for (const r of rows) {
    if (r.value !== null)
      out[r.key] = r.value
  }
  return out
}

// --- Pipeline global config (maxParallelOrchestrators, etc.) ---

export function setPipelineConfig(key: string, value: string, db: Database = getDb()): void {
  db.prepare(`
    INSERT INTO pipeline_config (key, value) VALUES (?, ?)
    ON CONFLICT(key) DO UPDATE SET value = excluded.value
  `).run(key, value)
}

export function getPipelineConfig(key: string, db: Database = getDb()): string | null {
  const row = db
    .prepare('SELECT value FROM pipeline_config WHERE key = ?')
    .get(key) as { value: string | null } | undefined
  return row?.value ?? null
}

export function getPipelineConfigNumber(
  key: string,
  fallback: number,
  db: Database = getDb(),
): number {
  const raw = getPipelineConfig(key, db)
  if (raw === null)
    return fallback
  const n = Number(raw)
  return Number.isFinite(n) ? n : fallback
}
