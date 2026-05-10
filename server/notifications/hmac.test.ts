import { describe, expect, it } from 'bun:test'
import { buildSignatureHeader, computeWebhookHmac } from './hmac.js'

describe('computeWebhookHmac', () => {
  it('returns a 64-char hex string', () => {
    const result = computeWebhookHmac('secret', 'payload')
    expect(result).toHaveLength(64)
    expect(result).toMatch(/^[0-9a-f]{64}$/)
  })

  it('is deterministic', () => {
    expect(computeWebhookHmac('s', 'p')).toBe(computeWebhookHmac('s', 'p'))
  })

  it('differs when secret changes', () => {
    expect(computeWebhookHmac('a', 'p')).not.toBe(computeWebhookHmac('b', 'p'))
  })

  it('differs when payload changes', () => {
    expect(computeWebhookHmac('s', 'a')).not.toBe(computeWebhookHmac('s', 'b'))
  })
})

describe('buildSignatureHeader', () => {
  it('returns sha256=<64hex> format', () => {
    expect(buildSignatureHeader('s', '1715000000', 'body')).toMatch(/^sha256=[0-9a-f]{64}$/)
  })

  it('incorporates timestamp into signed payload', () => {
    const a = buildSignatureHeader('s', '1715000000', 'body')
    const b = buildSignatureHeader('s', '1715000001', 'body')
    expect(a).not.toBe(b)
  })
})
