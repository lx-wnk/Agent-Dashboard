import { describe, expect, it } from 'vitest'
import { renderMarkdown } from './markdown'

describe('renderMarkdown', () => {
  describe('legitimate markdown rendering', () => {
    it('renders bold text', () => {
      expect(renderMarkdown('**bold**')).toContain('<strong>bold</strong>')
    })

    it('renders headings', () => {
      expect(renderMarkdown('# Title')).toMatch(/<h1[^>]*>Title<\/h1>/)
    })

    it('renders unordered lists', () => {
      const html = renderMarkdown('- one\n- two')
      expect(html).toContain('<ul>')
      expect(html).toContain('<li>one</li>')
      expect(html).toContain('<li>two</li>')
    })

    it('renders inline code', () => {
      expect(renderMarkdown('`code`')).toContain('<code>code</code>')
    })

    it('renders fenced code blocks (gfm)', () => {
      const html = renderMarkdown('```\nconst x = 1\n```')
      expect(html).toContain('<pre>')
      expect(html).toContain('<code>')
    })

    it('converts single newlines to <br> (breaks: true)', () => {
      expect(renderMarkdown('line one\nline two')).toContain('<br>')
    })

    it('renders safe links and keeps the href', () => {
      const html = renderMarkdown('[Example](https://example.com)')
      expect(html).toContain('href="https://example.com"')
      expect(html).toContain('>Example</a>')
    })
  })

  describe('xSS payload neutralization', () => {
    it('strips <script> tags', () => {
      const html = renderMarkdown('<script>alert(1)</script>')
      expect(html).not.toContain('<script')
      expect(html.toLowerCase()).not.toContain('alert(1)')
    })

    it('strips onerror handlers from <img>', () => {
      const html = renderMarkdown('<img src=x onerror=alert(1)>')
      expect(html).not.toContain('onerror')
      expect(html.toLowerCase()).not.toContain('alert(1)')
    })

    it('strips onclick handlers from <a>', () => {
      const html = renderMarkdown('<a href="#" onclick="alert(1)">click</a>')
      expect(html).not.toContain('onclick')
      expect(html.toLowerCase()).not.toContain('alert(1)')
    })

    it('strips javascript: URLs from markdown links', () => {
      // eslint-disable-next-line no-script-url
      const html = renderMarkdown('[click](javascript:alert(1))')
      expect(html.toLowerCase()).not.toContain('javascript:')
    })

    it('strips javascript: URLs from raw anchor tags', () => {
      // eslint-disable-next-line no-script-url
      const html = renderMarkdown('<a href="javascript:alert(1)">x</a>')
      expect(html.toLowerCase()).not.toContain('javascript:')
    })

    it('strips inline event handlers from arbitrary elements', () => {
      const html = renderMarkdown('<div onmouseover="alert(1)">hover</div>')
      expect(html).not.toContain('onmouseover')
      expect(html.toLowerCase()).not.toContain('alert(1)')
    })

    it('strips <iframe> tags', () => {
      const html = renderMarkdown('<iframe src="https://evil.example"></iframe>')
      expect(html).not.toContain('<iframe')
    })
  })

  it('returns an empty string for empty input', () => {
    expect(renderMarkdown('').trim()).toBe('')
  })
})
