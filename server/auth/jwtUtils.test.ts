import { Buffer } from 'node:buffer'
import { describe, expect, it } from 'vitest'
import { signJwt, verifyJwt } from './jwtUtils'

const SECRET = 'test-secret-at-least-32-bytes-long!!'

describe('jwtUtils', () => {
  it('signs and verifies a payload round-trip', () => {
    const token = signJwt({ sub: '123', login: 'alex', isAdmin: false }, SECRET, 3600)
    const payload = verifyJwt(token, SECRET)
    expect(payload?.sub).toBe('123')
    expect(payload?.login).toBe('alex')
    expect(payload?.isAdmin).toBe(false)
  })

  it('returns null for a tampered token', () => {
    const token = signJwt({ sub: '123', login: 'alex', isAdmin: false }, SECRET, 3600)
    const tampered = `${token.slice(0, -5)}XXXXX`
    expect(verifyJwt(tampered, SECRET)).toBeNull()
  })

  it('returns null for an expired token', () => {
    const token = signJwt({ sub: '1', login: 'x', isAdmin: false }, SECRET, -1)
    expect(verifyJwt(token, SECRET)).toBeNull()
  })

  it('returns null for a token signed with a different secret', () => {
    const token = signJwt({ sub: '1', login: 'x', isAdmin: false }, SECRET, 3600)
    expect(verifyJwt(token, 'wrong-secret')).toBeNull()
  })

  it('returns null for a token with a tampered alg claim', async () => {
    // Forge a token with alg: "none" but otherwise valid signature for SECRET
    const base64url = (s: string): string =>
      Buffer.from(s).toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
    const noneHeader = base64url(JSON.stringify({ alg: 'none', typ: 'JWT' }))
    const body = base64url(JSON.stringify({
      sub: '1',
      login: 'x',
      isAdmin: false,
      exp: Math.floor(Date.now() / 1000) + 3600,
    }))
    // Sign with the real secret so the signature itself would otherwise pass
    const { createHmac } = await import('node:crypto')
    const sig = createHmac('sha256', SECRET).update(`${noneHeader}.${body}`).digest('base64')
      .replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
    const forged = `${noneHeader}.${body}.${sig}`
    expect(verifyJwt(forged, SECRET)).toBeNull()
  })

  it('returns null for a token with a non-JWT typ claim', async () => {
    const base64url = (s: string): string =>
      Buffer.from(s).toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
    const wrongTypHeader = base64url(JSON.stringify({ alg: 'HS256', typ: 'NOT_JWT' }))
    const body = base64url(JSON.stringify({
      sub: '1',
      login: 'x',
      isAdmin: false,
      exp: Math.floor(Date.now() / 1000) + 3600,
    }))
    const { createHmac } = await import('node:crypto')
    const sig = createHmac('sha256', SECRET).update(`${wrongTypHeader}.${body}`).digest('base64')
      .replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
    const forged = `${wrongTypHeader}.${body}.${sig}`
    expect(verifyJwt(forged, SECRET)).toBeNull()
  })
})
