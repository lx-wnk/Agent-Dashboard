import { describe, expect, it } from 'vitest'
import { parsePushNotice } from './pushPayload'

describe('parsePushNotice', () => {
  it('parses a valid notice', () => {
    expect(parsePushNotice({ title: 'Approval needed', body: 'x: Bash', url: '/', tag: 'permission-t1' })).toEqual({
      title: 'Approval needed',
      body: 'x: Bash',
      url: '/',
      tag: 'permission-t1',
    })
  })

  it('returns null when title is missing', () => {
    expect(parsePushNotice({ body: 'x: Bash', url: '/', tag: 'permission-t1' })).toBeNull()
  })

  it('returns null for non-object input', () => {
    expect(parsePushNotice('not an object')).toBeNull()
    expect(parsePushNotice(null)).toBeNull()
    expect(parsePushNotice(undefined)).toBeNull()
  })
})
