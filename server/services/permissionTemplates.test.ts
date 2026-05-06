import { describe, expect, it } from 'bun:test'
import { isPermissionTemplate, listTemplateNames, PERMISSION_TEMPLATES, resolveTemplate } from './permissionTemplates.js'

describe('permissionTemplates', () => {
  it('exposes konzept_baseline alongside the existing four templates', () => {
    const names = listTemplateNames()
    expect(names).toContain('konzept_baseline')
    expect(names).toContain('feature_implementation')
    expect(names).toContain('research_only')
    expect(names).toContain('test_only')
    expect(names).toContain('review_only')
  })

  it('konzept_baseline includes file-ops + safe Bash patterns and EXCLUDES git push and curl', () => {
    const baseline = resolveTemplate('konzept_baseline')
    const tools = new Set(baseline.map(e => e.tool))
    expect(tools.has('Read')).toBe(true)
    expect(tools.has('Write')).toBe(true)
    expect(tools.has('Edit')).toBe(true)
    expect(tools.has('MultiEdit')).toBe(true)
    expect(tools.has('Glob')).toBe(true)
    expect(tools.has('Grep')).toBe(true)
    expect(tools.has('LS')).toBe(true)

    const bashPatterns = baseline.filter(e => e.tool === 'Bash').map(e => e.pattern ?? '')
    expect(bashPatterns).toContain('pnpm test*')
    expect(bashPatterns).toContain('pnpm typecheck*')
    expect(bashPatterns).toContain('pnpm lint*')
    expect(bashPatterns).toContain('pnpm build*')
    expect(bashPatterns).toContain('git status*')
    expect(bashPatterns).toContain('git diff*')
    expect(bashPatterns).toContain('git log*')
    expect(bashPatterns).toContain('git add*')
    expect(bashPatterns).toContain('git commit*')
    expect(bashPatterns).toContain('git checkout*')
    expect(bashPatterns).toContain('git branch*')

    expect(bashPatterns).not.toContain('git push*')
    expect(bashPatterns.some(p => p.includes('curl'))).toBe(false)
    expect(bashPatterns.some(p => p.includes('wget'))).toBe(false)
  })

  it('isPermissionTemplate accepts konzept_baseline', () => {
    expect(isPermissionTemplate('konzept_baseline')).toBe(true)
    expect(isPermissionTemplate('not_a_template')).toBe(false)
  })

  it('PERMISSION_TEMPLATES.konzept_baseline is non-empty', () => {
    expect(PERMISSION_TEMPLATES.konzept_baseline.length).toBeGreaterThan(0)
  })
})
