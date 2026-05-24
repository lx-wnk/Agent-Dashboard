import { describe, expect, it } from 'vitest'
import { sanitizeHtml, useSafeHtml } from './useSafeHtml'

describe('useSafeHtml', () => {
  it('strips <script> tags', () => {
    const { sanitizeHtml: s } = useSafeHtml()
    expect(s('<script>alert(1)</script>')).not.toContain('<script>')
  })

  it('strips inline event handlers', () => {
    expect(sanitizeHtml('<img src=x onerror="alert(1)">')).not.toContain('onerror')
  })

  it('neutralizes javascript: URLs', () => {
    const html = sanitizeHtml('<a href="javascript:alert(1)">x</a>')
    expect(html.toLowerCase()).not.toContain('javascript:')
  })

  it('drops <iframe>', () => {
    expect(sanitizeHtml('<iframe src="https://evil"></iframe>')).not.toContain('<iframe')
  })

  it('preserves safe HTML', () => {
    const html = sanitizeHtml('<p>hello <strong>world</strong></p>')
    expect(html).toContain('<p>')
    expect(html).toContain('<strong>')
  })

  it('returns empty string for non-string input', () => {
    expect(sanitizeHtml(null as unknown as string)).toBe('')
    expect(sanitizeHtml(undefined as unknown as string)).toBe('')
  })
})
