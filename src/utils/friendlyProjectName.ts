export function friendlyProjectName(slug: string): string {
  return slug
    .split(/[-_]+/)
    .map(w => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}
