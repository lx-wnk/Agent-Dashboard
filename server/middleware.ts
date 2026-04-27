import type { NextFunction, Request, Response } from 'express'
import { Buffer } from 'node:buffer'
import { timingSafeEqual } from 'node:crypto'
import process from 'node:process'

export function requireApiToken(req: Request, res: Response, next: NextFunction): void {
  const apiToken = process.env.DASHBOARD_API_TOKEN
  if (!apiToken) {
    next()
    return
  }
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
