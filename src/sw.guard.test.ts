import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// The worker is a build artefact, so these hold on its source: a fetch handler
// or a precache route reintroduced here is what made a restarted server answer
// 404 for the bundle the cached index.html named, while the status bar still
// reported the app up to date.
const source = readFileSync(resolve(process.cwd(), 'src/sw.ts'), 'utf8')

describe('service worker caching', () => {
  // Without this the new worker sits in "waiting" behind the old one, its
  // activate handler never runs, and the cache a previous build installed is
  // served on indefinitely — the fix would be inert on every existing install.
  it('takes over on install instead of waiting behind the old worker', () => {
    expect(source).toMatch(/addEventListener\(\s*['"]install['"][\s\S]{0,120}skipWaiting\(\)/)
  })

  it('serves nothing from a cache: no fetch handler', () => {
    expect(source).not.toMatch(/addEventListener\(\s*['"]fetch['"]/)
  })

  it('does not precache the build output', () => {
    // The import, not the word: the comments above explain why it is gone.
    expect(source).not.toMatch(/from\s+['"]workbox-precaching['"]/)
    expect(source).not.toMatch(/precacheAndRoute\s*\(/)
  })

  // Push, background sync and skip-waiting are why the worker still exists;
  // dropping the precache must not have taken them with it.
  it.each(['push', 'sync', 'notificationclick', 'message', 'activate', 'install'])(
    'keeps the %s handler',
    (name) => {
      expect(source).toMatch(new RegExp(`addEventListener\\(\\s*['"]${name}['"]`))
    },
  )
})
