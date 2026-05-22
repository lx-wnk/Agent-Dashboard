import DOMPurify from 'dompurify'
import { Marked } from 'marked'

/**
 * Shared markdown renderer (SSOT for the duplicated component wrappers).
 *
 * Parses markdown with a single `marked` instance, then sanitizes the
 * resulting HTML with DOMPurify before it reaches any `v-html` binding.
 * Dangerous tags/attributes (`<script>`, `onerror`, `javascript:` URLs, …)
 * are stripped, so callers can safely interpolate the output.
 *
 * Component-specific pre-processing (e.g. RefinementChat's `cleanContent`)
 * must run BEFORE calling this function — keep it local to the component.
 */
const md = new Marked({ breaks: true, gfm: true })

export function renderMarkdown(text: string): string {
  return DOMPurify.sanitize(md.parse(text, { async: false }) as string)
}
