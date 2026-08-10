/** Helper text spelling out the format SLUG_RE enforces. */
export const SLUG_FORMAT_HINT = 'Starts with a lowercase letter or digit, then lowercase letters, digits and hyphens, up to 64 characters.'

/** Helper text for a slug field that follows `source` until the user types in it. */
export function derivedSlugHint(source: string): string {
  return `Filled in from the ${source}; type here to take it over, clear it to hand it back. ${SLUG_FORMAT_HINT}`
}
