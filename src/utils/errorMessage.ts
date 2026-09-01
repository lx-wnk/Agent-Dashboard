/**
 * Extract a human-readable message from an unknown caught value.
 * Replaces the repeated `(err as Error).message` / `err instanceof Error ? ...`
 * idioms scattered across composables and components.
 */
export function errorMessage(err: unknown, fallback = 'Unknown error'): string {
  return err instanceof Error ? err.message : fallback
}

/**
 * Read an API error response's `error` field, falling back when the body is
 * absent or unparseable. A 403 in particular is an ordinary state here — the
 * server names the actual reason (missing grant, rate limit, grant-store
 * failure), so the caller renders that rather than asserting a cause of its
 * own.
 */
export async function readErrorMessage(res: Response, fallback: string): Promise<string> {
  const body = await res.json().catch(() => ({})) as { error?: string }
  return body.error || fallback
}
