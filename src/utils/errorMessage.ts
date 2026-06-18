/**
 * Extract a human-readable message from an unknown caught value.
 * Replaces the repeated `(err as Error).message` / `err instanceof Error ? ...`
 * idioms scattered across composables and components.
 */
export function errorMessage(err: unknown, fallback = 'Unknown error'): string {
  return err instanceof Error ? err.message : fallback
}
