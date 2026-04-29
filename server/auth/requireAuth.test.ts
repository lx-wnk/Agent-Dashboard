import type { NextFunction, Request, Response } from 'express'
import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { signJwt } from './jwtUtils.js'
import { isAuthEnabled, requireAuth } from './requireAuth.js'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const JWT_SECRET = 'a-sufficiently-long-test-secret-key-32b'

function makeRes() {
  const calls: { method: string, args: unknown[] }[] = []
  const res = {
    _statusCode: 0,
    _body: undefined as unknown,
    _clearedCookies: [] as string[],
    _calls: calls,
    status(code: number) {
      this._statusCode = code
      return this
    },
    json(body: unknown) {
      this._body = body
      return this
    },
    clearCookie(name: string, _opts?: unknown) {
      this._clearedCookies.push(name)
      return this
    },
  }
  return res
}

function makeReq(cookies: Record<string, string> = {}): Partial<Request> {
  return { cookies, headers: {} } as unknown as Partial<Request>
}

// ---------------------------------------------------------------------------
// isAuthEnabled
// ---------------------------------------------------------------------------

describe('isAuthEnabled', () => {
  afterEach(() => {
    delete process.env.GITHUB_CLIENT_ID
    delete process.env.GITHUB_CLIENT_SECRET
  })

  it('returns false when GITHUB_CLIENT_ID is not set', () => {
    delete process.env.GITHUB_CLIENT_ID
    delete process.env.GITHUB_CLIENT_SECRET
    expect(isAuthEnabled()).toBe(false)
  })

  it('returns false when only GITHUB_CLIENT_ID is set', () => {
    process.env.GITHUB_CLIENT_ID = 'some-client-id'
    delete process.env.GITHUB_CLIENT_SECRET
    expect(isAuthEnabled()).toBe(false)
  })

  it('returns false when only GITHUB_CLIENT_SECRET is set', () => {
    delete process.env.GITHUB_CLIENT_ID
    process.env.GITHUB_CLIENT_SECRET = 'some-secret'
    expect(isAuthEnabled()).toBe(false)
  })

  it('returns true when both GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET are set', () => {
    process.env.GITHUB_CLIENT_ID = 'some-client-id'
    process.env.GITHUB_CLIENT_SECRET = 'some-secret'
    expect(isAuthEnabled()).toBe(true)
  })
})

// ---------------------------------------------------------------------------
// requireAuth — auth disabled (fail-open as admin)
// ---------------------------------------------------------------------------

describe('requireAuth — auth disabled', () => {
  beforeEach(() => {
    delete process.env.GITHUB_CLIENT_ID
    delete process.env.GITHUB_CLIENT_SECRET
  })

  afterEach(() => {
    delete process.env.GITHUB_CLIENT_ID
    delete process.env.GITHUB_CLIENT_SECRET
  })

  it('sets req.user as local admin and calls next() when auth is off', () => {
    let nextCalled = false
    const next = () => { nextCalled = true }

    const req = makeReq()
    const res = makeRes()

    requireAuth(req as Request, res as unknown as Response, next as NextFunction)

    expect(nextCalled).toBe(true)
    expect((req as Request).user).toEqual({ id: 'standalone', login: 'local', isAdmin: true })
    expect(res._statusCode).toBe(0)
  })
})

// ---------------------------------------------------------------------------
// requireAuth — auth enabled
// ---------------------------------------------------------------------------

describe('requireAuth — auth enabled', () => {
  beforeEach(() => {
    process.env.GITHUB_CLIENT_ID = 'test-client-id'
    process.env.GITHUB_CLIENT_SECRET = 'test-client-secret'
    process.env.JWT_SECRET = JWT_SECRET
  })

  afterEach(() => {
    delete process.env.GITHUB_CLIENT_ID
    delete process.env.GITHUB_CLIENT_SECRET
    delete process.env.JWT_SECRET
  })

  it('returns 401 when no dashboard_session cookie is present', () => {
    let nextCalled = false
    const next = () => { nextCalled = true }

    const req = makeReq({})
    const res = makeRes()

    requireAuth(req as Request, res as unknown as Response, next as NextFunction)

    expect(nextCalled).toBe(false)
    expect(res._statusCode).toBe(401)
  })

  it('returns 500 when JWT_SECRET is not set', () => {
    delete process.env.JWT_SECRET

    let nextCalled = false
    const next = () => { nextCalled = true }

    const token = signJwt({ sub: '1', login: 'alex', isAdmin: false }, JWT_SECRET, 3600)
    const req = makeReq({ dashboard_session: token })
    const res = makeRes()

    requireAuth(req as Request, res as unknown as Response, next as NextFunction)

    expect(nextCalled).toBe(false)
    expect(res._statusCode).toBe(500)
  })

  it('returns 401 and clears cookie when token is invalid', () => {
    let nextCalled = false
    const next = () => { nextCalled = true }

    const req = makeReq({ dashboard_session: 'not.a.valid.jwt' })
    const res = makeRes()

    requireAuth(req as Request, res as unknown as Response, next as NextFunction)

    expect(nextCalled).toBe(false)
    expect(res._statusCode).toBe(401)
    expect(res._clearedCookies).toContain('dashboard_session')
  })

  it('returns 401 and clears cookie when token is expired', () => {
    let nextCalled = false
    const next = () => { nextCalled = true }

    const expired = signJwt({ sub: '1', login: 'alex', isAdmin: false }, JWT_SECRET, -60)
    const req = makeReq({ dashboard_session: expired })
    const res = makeRes()

    requireAuth(req as Request, res as unknown as Response, next as NextFunction)

    expect(nextCalled).toBe(false)
    expect(res._statusCode).toBe(401)
    expect(res._clearedCookies).toContain('dashboard_session')
  })

  it('sets req.user and calls next() for a valid JWT', () => {
    let nextCalled = false
    const next = () => { nextCalled = true }

    const token = signJwt({ sub: '42', login: 'testuser', isAdmin: true }, JWT_SECRET, 3600)
    const req = makeReq({ dashboard_session: token })
    const res = makeRes()

    requireAuth(req as Request, res as unknown as Response, next as NextFunction)

    expect(nextCalled).toBe(true)
    expect((req as Request).user).toEqual({ id: '42', login: 'testuser', isAdmin: true })
    expect(res._statusCode).toBe(0)
  })

  it('sets isAdmin: false correctly for non-admin users', () => {
    let nextCalled = false
    const next = () => { nextCalled = true }

    const token = signJwt({ sub: '99', login: 'normaluser', isAdmin: false }, JWT_SECRET, 3600)
    const req = makeReq({ dashboard_session: token })
    const res = makeRes()

    requireAuth(req as Request, res as unknown as Response, next as NextFunction)

    expect(nextCalled).toBe(true)
    expect((req as Request).user?.isAdmin).toBe(false)
  })
})
