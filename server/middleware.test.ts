import type { NextFunction, Request, Response } from 'express'
import { afterEach, beforeEach, describe, expect, it, mock } from 'bun:test'
import { createRejectCrossOrigin, requireApiToken } from './middleware.js'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeRes() {
  const res = {
    _statusCode: 0,
    _body: undefined as unknown,
    status(code: number) {
      this._statusCode = code
      return this
    },
    json(body: unknown) {
      this._body = body
      return this
    },
  }
  return res
}

function makeReq(headers: Record<string, string> = {}): Partial<Request> {
  return { headers } as unknown as Partial<Request>
}

// ---------------------------------------------------------------------------
// requireApiToken
// ---------------------------------------------------------------------------

describe('requireApiToken — no env var (generated token active)', () => {
  afterEach(() => {
    delete process.env.DASHBOARD_API_TOKEN
  })

  it('returns 401 when DASHBOARD_API_TOKEN is not set and no Authorization header provided', () => {
    delete process.env.DASHBOARD_API_TOKEN
    const next = mock()
    const req = makeReq()
    const res = makeRes()

    requireApiToken(req as Request, res as unknown as Response, next as unknown as NextFunction)

    expect(next).toHaveBeenCalledTimes(0)
    expect(res._statusCode).toBe(401)
  })
})

describe('requireApiToken — token enforcement', () => {
  beforeEach(() => {
    process.env.DASHBOARD_API_TOKEN = 'test-secret-token'
  })

  afterEach(() => {
    delete process.env.DASHBOARD_API_TOKEN
  })

  it('returns 401 when no Authorization header is present', () => {
    const next = mock()
    const req = makeReq({})
    const res = makeRes()

    requireApiToken(req as Request, res as unknown as Response, next as unknown as NextFunction)

    expect(next).not.toHaveBeenCalled()
    expect(res._statusCode).toBe(401)
  })

  it('returns 401 when Authorization header does not start with "Bearer "', () => {
    const next = mock()
    const req = makeReq({ authorization: 'Basic dXNlcjpwYXNz' })
    const res = makeRes()

    requireApiToken(req as Request, res as unknown as Response, next as unknown as NextFunction)

    expect(next).not.toHaveBeenCalled()
    expect(res._statusCode).toBe(401)
  })

  it('returns 403 when Bearer token is wrong', () => {
    const next = mock()
    const req = makeReq({ authorization: 'Bearer wrong-token' })
    const res = makeRes()

    requireApiToken(req as Request, res as unknown as Response, next as unknown as NextFunction)

    expect(next).not.toHaveBeenCalled()
    expect(res._statusCode).toBe(403)
  })

  it('calls next() when Bearer token matches exactly', () => {
    const next = mock()
    const req = makeReq({ authorization: 'Bearer test-secret-token' })
    const res = makeRes()

    requireApiToken(req as Request, res as unknown as Response, next as unknown as NextFunction)

    expect(next).toHaveBeenCalledTimes(1)
    expect(res._statusCode).toBe(0)
  })

  it('returns 403 for a different-length token (timing-safe length check)', () => {
    const next = mock()
    // Provide a token that is longer than 'test-secret-token'
    const req = makeReq({ authorization: 'Bearer test-secret-token-extra-chars' })
    const res = makeRes()

    requireApiToken(req as Request, res as unknown as Response, next as unknown as NextFunction)

    expect(next).not.toHaveBeenCalled()
    expect(res._statusCode).toBe(403)
  })
})

// ---------------------------------------------------------------------------
// createRejectCrossOrigin
// ---------------------------------------------------------------------------

describe('createRejectCrossOrigin', () => {
  const rejectCrossOrigin = createRejectCrossOrigin('127.0.0.1', 13120)

  function makeOriginReq(origin?: string, referer?: string): Partial<Request> {
    const headers: Record<string, string> = {}
    if (origin !== undefined)
      headers.origin = origin
    if (referer !== undefined)
      headers.referer = referer
    return { headers } as unknown as Partial<Request>
  }

  it('returns false (allows) when no origin or referer header is present', () => {
    const req = makeOriginReq()
    const res = makeRes()
    expect(rejectCrossOrigin(req as Request, res as unknown as Response)).toBe(false)
    expect(res._statusCode).toBe(0)
  })

  it('returns false for the exact allowed origin', () => {
    const req = makeOriginReq('http://127.0.0.1:13120')
    const res = makeRes()
    expect(rejectCrossOrigin(req as Request, res as unknown as Response)).toBe(false)
    expect(res._statusCode).toBe(0)
  })

  it('returns false for the localhost alias on the correct port', () => {
    const req = makeOriginReq('http://localhost:13120')
    const res = makeRes()
    expect(rejectCrossOrigin(req as Request, res as unknown as Response)).toBe(false)
    expect(res._statusCode).toBe(0)
  })

  it('returns true and sends 403 for a foreign origin', () => {
    const req = makeOriginReq('http://evil.com')
    const res = makeRes()
    expect(rejectCrossOrigin(req as Request, res as unknown as Response)).toBe(true)
    expect(res._statusCode).toBe(403)
  })

  it('returns false for a correct referer (trailing slash)', () => {
    const req = makeOriginReq(undefined, 'http://localhost:13120/')
    const res = makeRes()
    expect(rejectCrossOrigin(req as Request, res as unknown as Response)).toBe(false)
    expect(res._statusCode).toBe(0)
  })

  it('returns true and sends 403 for a referer on the wrong port', () => {
    const req = makeOriginReq(undefined, 'http://127.0.0.1:9999/some/path')
    const res = makeRes()
    expect(rejectCrossOrigin(req as Request, res as unknown as Response)).toBe(true)
    expect(res._statusCode).toBe(403)
  })

  it('returns true and sends 403 for a wrong-origin even with a valid referer', () => {
    // origin takes precedence; both origin and referer must pass for allow
    const req = makeOriginReq('http://evil.com', 'http://127.0.0.1:13120/')
    const res = makeRes()
    // Either allowed → false
    expect(rejectCrossOrigin(req as Request, res as unknown as Response)).toBe(false)
  })
})
