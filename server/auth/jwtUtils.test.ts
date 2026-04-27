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
})
