import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// The Go server and the TypeScript client cannot share a module, so the two
// halves of this contract are kept in step by hand. Renaming the fixtures in
// GrantSettings.test.ts alongside the component keeps that suite green whatever
// the server actually sends, which is how the snake_case drift survived a full
// wire-format sweep — this reads the Go tags instead.
const GO_HANDLER_PATH = join(process.cwd(), 'server/internal/api/grants/handler.go')
const TS_CLIENT_PATH = join(process.cwd(), 'src/features/settings/composables/useGrants.ts')

function readSource(path: string, label: string): string {
  if (!existsSync(path))
    throw new Error(`grants-wire-parity: ${label} not found at ${path} — file moved? Update the path constant.`)
  return readFileSync(path, 'utf-8')
}

function goStructBody(source: string, name: string): string {
  const match = source.match(new RegExp(`type ${name} struct \\{([\\s\\S]*?)\\n\\}`))
  if (!match)
    throw new Error(`grants-wire-parity: could not find "type ${name} struct" in ${GO_HANDLER_PATH} — declaration changed shape.`)
  return match[1]
}

function jsonTagsOf(structBody: string): string[] {
  return [...structBody.matchAll(/json:"([^",]+)/g)].map(m => m[1])
}

function tsInterfaceKeys(source: string, name: string): string[] {
  const match = source.match(new RegExp(`export interface ${name} \\{([\\s\\S]*?)\\n\\}`))
  if (!match)
    throw new Error(`grants-wire-parity: could not find "export interface ${name}" in ${TS_CLIENT_PATH} — declaration changed shape.`)
  return [...match[1].matchAll(/^\s{2}(\w+)\??:/gm)].map(m => m[1])
}

describe('grants wire-format parity (Go <-> TS)', () => {
  const go = readSource(GO_HANDLER_PATH, 'grants handler')
  const ts = readSource(TS_CLIENT_PATH, 'useGrants')

  it('the Grant interface declares exactly the keys grantResponse emits', () => {
    const goTags = jsonTagsOf(goStructBody(go, 'grantResponse'))
    expect(goTags.length).toBeGreaterThan(0)
    expect(tsInterfaceKeys(ts, 'Grant').sort()).toEqual(goTags.sort())
  })

  it('the Capability interface declares exactly the keys capabilityResponse emits', () => {
    const goTags = jsonTagsOf(goStructBody(go, 'capabilityResponse'))
    expect(goTags.length).toBeGreaterThan(0)
    expect(tsInterfaceKeys(ts, 'Capability').sort()).toEqual(goTags.sort())
  })

  // omitempty is what dropped limitCount 0 (unlimited) and an empty contextRef
  // (global scope) from the payload while a typed decode on either side still
  // succeeded.
  it('neither response struct reintroduces omitempty', () => {
    for (const name of ['grantResponse', 'capabilityResponse'])
      expect(goStructBody(go, name)).not.toContain('omitempty')
  })

  it('the request body the client sends is still accepted by createGrantRequest', () => {
    const goTags = jsonTagsOf(goStructBody(go, 'createGrantRequest'))
    for (const key of tsInterfaceKeys(ts, 'CreateGrantInput'))
      expect(goTags).toContain(key)
  })
})
