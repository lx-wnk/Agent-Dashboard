import type { Database } from 'better-sqlite3'
import type { ApiKey, McpScope } from '../../src/types.js'
import { randomUUID } from 'node:crypto'
import { getDb } from './client.js'
import { type ApiKeyRow, rowToApiKey } from './rowMappers.js'

export interface CreateApiKeyInput {
  name: string
  keyHash: string
  scopes: McpScope[]
}

function nowIso(): string {
  return new Date().toISOString()
}

export function createApiKey(input: CreateApiKeyInput, db: Database = getDb()): ApiKey {
  const id = randomUUID()
  db.prepare(`
    INSERT INTO api_keys (id, name, key_hash, scopes, active, created_at)
    VALUES (@id, @name, @key_hash, @scopes, 1, @created_at)
  `).run({
    id,
    name: input.name,
    key_hash: input.keyHash,
    scopes: JSON.stringify(input.scopes),
    created_at: nowIso(),
  })
  return getApiKeyById(id, db)!
}

export function getApiKeyById(id: string, db: Database = getDb()): ApiKey | null {
  const row = db.prepare('SELECT * FROM api_keys WHERE id = ?').get(id) as ApiKeyRow | undefined
  return row ? rowToApiKey(row) : null
}

// Only returns active keys — inactive keys are treated as non-existent for auth purposes
export function getApiKeyByHash(hash: string, db: Database = getDb()): ApiKey | null {
  const row = db.prepare('SELECT * FROM api_keys WHERE key_hash = ? AND active = 1').get(hash) as ApiKeyRow | undefined
  return row ? rowToApiKey(row) : null
}

export function listApiKeys(opts: { includeRevoked?: boolean } = {}, db: Database = getDb()): ApiKey[] {
  const sql = opts.includeRevoked
    ? 'SELECT * FROM api_keys ORDER BY created_at DESC'
    : 'SELECT * FROM api_keys WHERE active = 1 ORDER BY created_at DESC'
  const rows = db.prepare(sql).all() as ApiKeyRow[]
  return rows.map(rowToApiKey)
}

export function revokeApiKey(id: string, db: Database = getDb()): boolean {
  const result = db.prepare('UPDATE api_keys SET active = 0 WHERE id = ?').run(id)
  return result.changes > 0
}

export function revokeApiKeyByName(name: string, db: Database = getDb()): boolean {
  const result = db.prepare('UPDATE api_keys SET active = 0 WHERE name = ?').run(name)
  return result.changes > 0
}

/**
 * Insert or replace a stage-run API key. Used by stage handlers so that a
 * re-keyed iterate run (same stageRun.id, new token) replaces the prior key
 * without hitting the UNIQUE name constraint.
 */
export function upsertStageRunApiKey(input: CreateApiKeyInput, db: Database = getDb()): ApiKey {
  const id = randomUUID()
  db.prepare(`
    INSERT INTO api_keys (id, name, key_hash, scopes, active, created_at)
    VALUES (@id, @name, @key_hash, @scopes, 1, @created_at)
    ON CONFLICT(name) DO UPDATE SET
      id = excluded.id,
      key_hash = excluded.key_hash,
      scopes = excluded.scopes,
      active = 1,
      created_at = excluded.created_at,
      last_used_at = NULL
  `).run({
    id,
    name: input.name,
    key_hash: input.keyHash,
    scopes: JSON.stringify(input.scopes),
    created_at: nowIso(),
  })
  return getApiKeyById(id, db)!
}

export function touchApiKey(id: string, db: Database = getDb()): void {
  db.prepare('UPDATE api_keys SET last_used_at = ? WHERE id = ?').run(nowIso(), id)
}
