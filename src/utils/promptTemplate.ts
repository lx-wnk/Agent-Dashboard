// Parse {{name}} tokens from a template body; returns unique token names in order.
export function parsePlaceholders(body: string): string[] {
  const seen = new Set<string>()
  const names: string[] = []
  for (const m of body.matchAll(/\{\{([^}]+)\}\}/g)) {
    const token = m[1].trim()
    if (!seen.has(token)) {
      seen.add(token)
      names.push(token)
    }
  }
  return names
}

// Replace all {{name}} occurrences with the corresponding fill value.
export function fillPlaceholders(body: string, fills: Record<string, string>): string {
  return body.replace(/\{\{([^}]+)\}\}/g, (_, token) => fills[token.trim()] ?? `{{${token}}}`)
}
