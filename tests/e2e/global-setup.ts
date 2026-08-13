import type { FullConfig } from '@playwright/test'

/**
 * The suite runs against a scratch database, which starts with the first-run
 * onboarding dialog open — it covers the page and swallows every click the
 * specs make. Runs after the webServer is ready, so the server is reachable.
 */
export default async function globalSetup(config: FullConfig): Promise<void> {
  const baseURL = config.projects[0]?.use?.baseURL
  if (!baseURL)
    throw new Error('global-setup: no baseURL configured')

  const res = await fetch(`${baseURL}/api/onboarding/status`, {
    method: 'PATCH',
    // The server rejects mutations whose Origin does not match its own host.
    headers: { 'Content-Type': 'application/json', 'Origin': baseURL },
    body: JSON.stringify({ completed: true }),
  })
  if (!res.ok)
    throw new Error(`global-setup: dismissing onboarding failed (${res.status})`)
}
