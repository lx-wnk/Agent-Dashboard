import type { NextFunction, Request, Response } from 'express'
import process from 'node:process'
import { verifyJwt } from './jwtUtils.js'

declare global {
  // eslint-disable-next-line ts/no-namespace
  namespace Express {
    interface Request {
      user?: { id: string, login: string, isAdmin: boolean }
    }
  }
}

export function isAuthEnabled(): boolean {
  return !!(process.env.GITHUB_CLIENT_ID && process.env.GITHUB_CLIENT_SECRET)
}

export function requireAuth(req: Request, res: Response, next: NextFunction): void {
  if (!isAuthEnabled()) {
    req.user = { id: 'standalone', login: 'local', isAdmin: true }
    next()
    return
  }
  const token = (req as unknown as { cookies?: Record<string, string> }).cookies?.dashboard_session
  if (!token) {
    res.status(401).json({ error: 'Not authenticated' })
    return
  }
  const secret = process.env.JWT_SECRET
  if (!secret) {
    console.error('[auth] JWT_SECRET is not set — rejecting all requests')
    res.status(500).json({ error: 'Server misconfiguration' })
    return
  }
  const payload = verifyJwt(token, secret)
  if (!payload) {
    res.clearCookie('dashboard_session', { httpOnly: true, sameSite: 'lax', path: '/' })
    res.status(401).json({ error: 'Session expired' })
    return
  }
  req.user = { id: payload.sub, login: payload.login, isAdmin: payload.isAdmin }
  next()
}
