import { readdirSync, readFileSync, existsSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const ROOT = process.cwd()
const SCHEMA_PATH = join(ROOT, 'plugin-sdk', 'plugin.schema.json')
const PLUGINS_DIR = join(ROOT, 'plugins')

const ID_RE = /^[a-z0-9][a-z0-9-]*$/
const CAPS = new Set(['auth_provider', 'route_extension', 'ui_extension'])
const SETTING_TYPES = new Set(['string', 'url', 'int', 'bool', 'enum'])
const SLOT_MODES = new Set(['override', 'extend'])

// Minimal structural validator mirroring plugin.schema.json's key rules.
// (No ajv dependency — see the SP5 spec, decision D4.)
function validateManifest(m: any): string[] {
  const errs: string[] = []
  if (typeof m.id !== 'string' || !ID_RE.test(m.id))
    errs.push(`id "${m.id}" must match ${ID_RE}`)
  if (m.capabilities !== undefined) {
    if (!Array.isArray(m.capabilities))
      errs.push('capabilities must be an array')
    else for (const c of m.capabilities) if (!CAPS.has(c)) errs.push(`unknown capability "${c}"`)
  }
  for (const s of m.settings ?? []) {
    if (typeof s.key !== 'string') errs.push('setting.key required')
    if (!SETTING_TYPES.has(s.type)) errs.push(`setting "${s.key}" bad type "${s.type}"`)
  }
  for (const sl of m.slots ?? []) {
    if (typeof sl.slot !== 'string') errs.push('slot.slot required')
    if (sl.mode !== undefined && !SLOT_MODES.has(sl.mode)) errs.push(`slot "${sl.slot}" bad mode "${sl.mode}"`)
  }
  return errs
}

describe('plugin-sdk', () => {
  it('ships a valid JSON schema for plugin.json', () => {
    expect(existsSync(SCHEMA_PATH)).toBe(true)
    const schema = JSON.parse(readFileSync(SCHEMA_PATH, 'utf8'))
    expect(schema.$schema).toContain('json-schema.org')
    expect(schema.required).toContain('id')
  })

  it('every example manifest satisfies the schema rules', () => {
    const dirs = readdirSync(PLUGINS_DIR, { withFileTypes: true }).filter(d => d.isDirectory())
    expect(dirs.length).toBeGreaterThan(0)
    for (const d of dirs) {
      const p = join(PLUGINS_DIR, d.name, 'plugin.json')
      if (!existsSync(p))
        continue
      const m = JSON.parse(readFileSync(p, 'utf8'))
      expect(validateManifest(m), `${d.name}/plugin.json`).toEqual([])
      expect(m.$schema, `${d.name} should reference the schema`).toBeTruthy()
    }
  })
})
