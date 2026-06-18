export const BRANCH_MAX = 20

export function truncateBranch(branch: string, max = BRANCH_MAX): string {
  return branch.length > max ? `${branch.slice(0, max - 1)}…` : branch
}

export interface EditorScheme {
  id: string
  label: string
  build: (absPath: string) => string
}

export const EDITOR_SCHEMES: EditorScheme[] = [
  { id: 'vscode', label: 'VS Code', build: p => `vscode://file${p}` },
  { id: 'cursor', label: 'Cursor', build: p => `cursor://file${p}` },
  { id: 'file', label: 'File', build: p => `file://${p}` },
]

export const DEFAULT_EDITOR_SCHEME = 'vscode'

export function editorHref(path: string | null | undefined, schemeId: string): string | null {
  if (!path)
    return null
  const scheme = EDITOR_SCHEMES.find(s => s.id === schemeId)
    ?? EDITOR_SCHEMES.find(s => s.id === DEFAULT_EDITOR_SCHEME)!
  return scheme.build(path)
}

const EDITOR_SCHEME_STORAGE_KEY = 'worktree.editorScheme'

export function loadEditorScheme(): string {
  try {
    const stored = localStorage.getItem(EDITOR_SCHEME_STORAGE_KEY)
    if (stored && EDITOR_SCHEMES.some(s => s.id === stored))
      return stored
  }
  catch {}
  return DEFAULT_EDITOR_SCHEME
}

export function saveEditorScheme(id: string): void {
  try {
    localStorage.setItem(EDITOR_SCHEME_STORAGE_KEY, id)
  }
  catch {}
}
