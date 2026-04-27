import { createHmac, timingSafeEqual } from 'node:crypto'

export interface JwtPayload {
  sub: string // GitHub numeric user ID
  login: string // GitHub username (display only)
  isAdmin: boolean
  exp: number // Unix timestamp
}

function base64url(buf: Buffer | string): string {
  const b = typeof buf === 'string' ? Buffer.from(buf) : buf
  return b.toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}

function sign(data: string, secret: string): string {
  return base64url(createHmac('sha256', secret).update(data).digest())
}

export function signJwt(payload: Omit<JwtPayload, 'exp'>, secret: string, expiresInSeconds: number): string {
  const header = base64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }))
  const body = base64url(JSON.stringify({ ...payload, exp: Math.floor(Date.now() / 1000) + expiresInSeconds }))
  const sig = sign(`${header}.${body}`, secret)
  return `${header}.${body}.${sig}`
}

export function verifyJwt(token: string, secret: string): JwtPayload | null {
  try {
    const parts = token.split('.')
    if (parts.length !== 3)
      return null
    const [header, body, sig] = parts
    const expected = sign(`${header}.${body}`, secret)
    const expectedBuf = Buffer.from(expected)
    const sigBuf = Buffer.from(sig)
    if (expectedBuf.length !== sigBuf.length || !timingSafeEqual(expectedBuf, sigBuf))
      return null
    const payload: JwtPayload = JSON.parse(Buffer.from(body, 'base64').toString())
    if (payload.exp < Math.floor(Date.now() / 1000))
      return null
    return payload
  }
  catch {
    return null
  }
}
