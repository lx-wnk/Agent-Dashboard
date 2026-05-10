import type { NextFunction, Request, Response } from 'express'
import { Buffer } from 'node:buffer'
import { randomBytes } from 'node:crypto'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { timingSafeEqual } from 'node:crypto'
import process from 'node:process'
import { consola } from 'consola'

const FALLBACK_TOKEN_PATH = join(homedir(), '.claude', 'dashboard-api-token')

let _resolvedApiToken: string | null = null

function resolveApiToken(): string {
  // Always check env var fresh — allows tests to override between calls
  const envToken = process.env.DASHBOARD_API_TOKEN
  if (envToken)
    return envToken

  // Only cache the generated/persisted token (not the env var)
  if (_resolvedApiToken)
    return _resolvedApiToken

  // No env var — use or generate a persisted token at mode 0600
  if (existsSync(FALLBACK_TOKEN_PATH)) {
    try {
      _resolvedApiToken = readFileSync(FALLBACK_TOKEN_PATH, 'utf-8').trim()
      if (_resolvedApiToken)
        return _resolvedApiToken
    }
    catch {
      // fall through to generate
    }
  }

  const generated = randomBytes(32).toString('hex')
  try {
    writeFileSync(FALLBACK_TOKEN_PATH, generated, { mode: 0o600 })
  }
  catch (err) {
    consola.warn('[config] Could not write dashboard-api-token file:', (err as Error).message)
  }
  consola.warn(
    `[config] DASHBOARD_API_TOKEN is not set — generated token written to ${FALLBACK_TOKEN_PATH}.\n`
    + `  To use it: export DASHBOARD_API_TOKEN=$(cat ${FALLBACK_TOKEN_PATH})`,
  )
  _resolvedApiToken = generated
  return generated
}

export function requireApiToken(req: Request, res: Response, next: NextFunction): void {
  const apiToken = resolveApiToken()
  const auth = req.headers.authorization
  if (!auth?.startsWith('Bearer ')) {
    res.status(401).json({ error: 'Missing Authorization header' })
    return
  }
  const provided = Buffer.from(auth.slice(7))
  const expected = Buffer.from(apiToken)
  if (provided.length !== expected.length || !timingSafeEqual(provided, expected)) {
    res.status(403).json({ error: 'Invalid token' })
    return
  }
  next()
}

// CSRF protection for mutation endpoints
export function createRejectCrossOrigin(host: string, port: number): (req: Request, res: Response) => boolean {
  return function rejectCrossOrigin(req: Request, res: Response): boolean {
    const origin = req.headers.origin || ''
    const referer = req.headers.referer || ''
    // Allow requests with no origin (non-browser clients like curl)
    if (!origin && !referer)
      return false
    const allowed = (s: string) => {
      try {
        const url = new URL(s)
        return (url.hostname === host || url.hostname === 'localhost' || url.hostname === '127.0.0.1') && url.port === String(port)
      }
      catch {
        return false
      }
    }
    if (allowed(origin) || allowed(referer))
      return false
    res.status(403).json({ error: 'Cross-origin request blocked' })
    return true
  }
}
