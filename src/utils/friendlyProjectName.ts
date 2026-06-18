const SEPARATOR_RE = /[-_]+/

export function friendlyProjectName(slug: string): string {
  return slug
    .split(SEPARATOR_RE)
    .map(w => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}
