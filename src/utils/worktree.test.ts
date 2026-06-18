import { beforeEach, describe, expect, it } from 'vitest'

import {
  BRANCH_MAX,
  DEFAULT_EDITOR_SCHEME,
  editorHref,
  loadEditorScheme,
  saveEditorScheme,
  truncateBranch,
} from './worktree'

const store: Record<string, string> = {}
globalThis.localStorage = {
  getItem: (k: string) => store[k] ?? null,
  setItem: (k: string, v: string) => { store[k] = v },
  removeItem: (k: string) => { delete store[k] },
  clear: () => { Object.keys(store).forEach(k => delete store[k]) },
  length: 0,
  key: () => null,
}

describe('truncateBranch', () => {
  it('leaves short branches unchanged', () => {
    expect(truncateBranch('main')).toBe('main')
    expect(truncateBranch('feat/short')).toBe('feat/short')
  })

  it('leaves branch exactly at max unchanged', () => {
    const exact = 'a'.repeat(BRANCH_MAX)
    expect(truncateBranch(exact)).toBe(exact)
  })

  it('truncates long branch to length ≤ max and appends ellipsis', () => {
    const long = 'feat/very-long-branch-name-here'
    const result = truncateBranch(long)
    expect(result.length).toBeLessThanOrEqual(BRANCH_MAX)
    expect(result.endsWith('…')).toBe(true)
  })

  it('respects a custom max parameter', () => {
    const result = truncateBranch('abcdefghij', 5)
    expect(result.length).toBeLessThanOrEqual(5)
    expect(result.endsWith('…')).toBe(true)
  })
})

describe('editorHref', () => {
  it('returns null for null path', () => {
    expect(editorHref(null, 'vscode')).toBeNull()
  })

  it('returns null for undefined path', () => {
    expect(editorHref(undefined, 'vscode')).toBeNull()
  })

  it('returns null for empty string path', () => {
    expect(editorHref('', 'vscode')).toBeNull()
  })

  it('builds a vscode URI', () => {
    expect(editorHref('/Users/alex/project', 'vscode')).toBe('vscode://file/Users/alex/project')
  })

  it('builds a cursor URI', () => {
    expect(editorHref('/Users/alex/project', 'cursor')).toBe('cursor://file/Users/alex/project')
  })

  it('builds a file URI', () => {
    expect(editorHref('/Users/alex/project', 'file')).toBe('file:///Users/alex/project')
  })

  it('falls back to vscode for unknown scheme id', () => {
    expect(editorHref('/Users/alex/project', 'unknown-editor')).toBe('vscode://file/Users/alex/project')
  })
})

describe('loadEditorScheme / saveEditorScheme', () => {
  beforeEach(() => localStorage.clear())

  it('returns DEFAULT_EDITOR_SCHEME when storage is empty', () => {
    expect(loadEditorScheme()).toBe(DEFAULT_EDITOR_SCHEME)
  })

  it('returns DEFAULT_EDITOR_SCHEME for an invalid stored value', () => {
    localStorage.setItem('worktree.editorScheme', 'sublime')
    expect(loadEditorScheme()).toBe(DEFAULT_EDITOR_SCHEME)
  })

  it('round-trips a valid scheme id', () => {
    saveEditorScheme('cursor')
    expect(loadEditorScheme()).toBe('cursor')
  })

  it('round-trips file scheme', () => {
    saveEditorScheme('file')
    expect(loadEditorScheme()).toBe('file')
  })

  it('round-trips vscode scheme', () => {
    saveEditorScheme('vscode')
    expect(loadEditorScheme()).toBe('vscode')
  })
})
