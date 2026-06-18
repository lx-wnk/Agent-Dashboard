// Resolves design-system color tokens to concrete rgb() strings for d3/SVG
// charts, which cannot consume CSS custom properties directly. A hidden probe
// element is used so nested `var()` references resolve fully (getComputedStyle
// on a custom property may return an unresolved `var(...)` string otherwise).
// Call inside a render that also depends on the active theme so colors refresh
// when the `.dark` class toggles.

function resolveVar(name: string): string {
  if (typeof document === 'undefined')
    return ''
  const probe = document.createElement('span')
  probe.style.color = `var(${name})`
  probe.style.display = 'none'
  document.body.appendChild(probe)
  const value = getComputedStyle(probe).color
  probe.remove()
  return value
}

export interface ChartColors {
  accent: string
  info: string
  success: string
  warning: string
  danger: string
  line: string
  lineStrong: string
  fg: string
  fgMute: string
  fgFaint: string
}

export function chartColors(): ChartColors {
  return {
    accent: resolveVar('--accent'),
    info: resolveVar('--info'),
    success: resolveVar('--success'),
    warning: resolveVar('--warning'),
    danger: resolveVar('--danger'),
    line: resolveVar('--line'),
    lineStrong: resolveVar('--line-strong'),
    fg: resolveVar('--fg'),
    fgMute: resolveVar('--fg-mute'),
    fgFaint: resolveVar('--fg-faint'),
  }
}

// Ordinal palette for multi-category series (models, tools). Cycles the
// semantic hues; index past the end wraps.
export function chartPalette(): string[] {
  const c = chartColors()
  return [c.accent, c.info, c.success, c.warning, c.danger]
}

export function paletteColor(index: number): string {
  const p = chartPalette()
  return p[((index % p.length) + p.length) % p.length]
}
