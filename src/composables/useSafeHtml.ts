import type { Config as DOMPurifyConfig } from 'dompurify'
import DOMPurify from 'dompurify'

/**
 * Safe-HTML composable — XSS-hardened wrapper around DOMPurify.
 *
 * Use `sanitizeHtml` whenever a `v-html` binding receives content that
 * is even partially derived from user-controlled input (agent transcripts,
 * refine messages, task notes, memory bodies). Markdown-rendered HTML is
 * already sanitized at the `src/utils/markdown.ts` chokepoint — this
 * composable covers the cases where pre-rendered HTML reaches `v-html`
 * without going through that pipeline.
 *
 * Defaults strip every script/style/iframe/object/event-handler so that a
 * payload like `<script>alert(1)</script>` or
 * `<img src=x onerror=alert(1)>` cannot execute in the dashboard.
 *
 * Pass `options` to extend the allow-list for specific call sites (e.g.
 * embedded SVGs) — but never disable sanitization entirely.
 */
export function useSafeHtml() {
  function sanitizeHtml(input: string, options?: DOMPurifyConfig): string {
    if (typeof input !== 'string')
      return ''
    return DOMPurify.sanitize(input, options ?? {}) as string
  }

  return { sanitizeHtml }
}

/**
 * One-shot helper for call sites that do not need a composable.
 */
export function sanitizeHtml(input: string, options?: DOMPurifyConfig): string {
  return useSafeHtml().sanitizeHtml(input, options)
}
