import { describe, expect, it } from 'vitest'

// Resolved token hex values — keep in sync with src/styles/main.css
// (--warning-text / --warning-soft under :root and .dark).
const LIGHT_WARNING_TEXT = '#a16207' // yellow-700
const LIGHT_WARNING_SOFT = '#fef9c3'
const DARK_WARNING_TEXT = '#facc15' // yellow-400
const DARK_CARD = '#12151c'
const DARK_WARNING_SOFT_ALPHA = 0.16 // color-mix(in oklch, yellow-400 16%, transparent) over --card

function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '')
  return [Number.parseInt(h.slice(0, 2), 16), Number.parseInt(h.slice(2, 4), 16), Number.parseInt(h.slice(4, 6), 16)]
}

function relativeLuminance([r, g, b]: [number, number, number]): number {
  const channel = (c: number) => {
    const s = c / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
}

function contrastRatio(fg: string, bg: string): number {
  const l1 = relativeLuminance(hexToRgb(fg))
  const l2 = relativeLuminance(hexToRgb(bg))
  const lighter = Math.max(l1, l2)
  const darker = Math.min(l1, l2)
  return (lighter + 0.05) / (darker + 0.05)
}

function blend(fgHex: string, alpha: number, bgHex: string): [number, number, number] {
  const [fr, fg, fb] = hexToRgb(fgHex)
  const [br, bg, bb] = hexToRgb(bgHex)
  return [fr * alpha + br * (1 - alpha), fg * alpha + bg * (1 - alpha), fb * alpha + bb * (1 - alpha)]
}

function rgbToHex([r, g, b]: [number, number, number]): string {
  return `#${[r, g, b].map(c => Math.round(c).toString(16).padStart(2, '0')).join('')}`
}

// A11Y-9: the "needs-you" pipeline column swapped ad-hoc translucent yellow
// utilities for the warning-text/warning-soft design tokens. Regression guard
// against a future token rename silently dropping below WCAG AA (4.5:1).
describe('pipelineBoard needs-you column — warning token contrast', () => {
  it('meets 4.5:1 for warning-text on warning-soft in light mode', () => {
    expect(contrastRatio(LIGHT_WARNING_TEXT, LIGHT_WARNING_SOFT)).toBeGreaterThanOrEqual(4.5)
  })

  it('meets 4.5:1 for warning-text on warning-soft in dark mode', () => {
    const darkWarningSoft = rgbToHex(blend(DARK_WARNING_TEXT, DARK_WARNING_SOFT_ALPHA, DARK_CARD))
    expect(contrastRatio(DARK_WARNING_TEXT, darkWarningSoft)).toBeGreaterThanOrEqual(4.5)
  })
})
