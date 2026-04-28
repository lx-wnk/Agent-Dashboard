import { mkdtemp, realpath, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { encodePath } from '../jsonlParser.js'
import {
  extractJsonBlock,
  findNewestSessionId,
  lastAssistantText,
  resolvedProjectDir,
} from './sessionOutputReader.js'

describe('resolvedProjectDir (symlink handling)', () => {
  let realDir: string
  let linkDir: string

  beforeEach(async () => {
    realDir = await mkdtemp(join(tmpdir(), 'dashboard-real-'))
    linkDir = join(tmpdir(), `dashboard-link-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`)
    await symlink(realDir, linkDir)
  })

  afterEach(async () => {
    await rm(linkDir, { force: true })
    await rm(realDir, { recursive: true, force: true })
  })

  it('encodes the realpath when the cwd is a symlink, not the symlink path itself', async () => {
    const resolved = await resolvedProjectDir(linkDir)
    const trueReal = await realpath(realDir)
    // Claude CLI writes to the realpath-encoded directory, so we must match it.
    expect(resolved.endsWith(encodePath(trueReal))).toBe(true)
    expect(resolved.endsWith(encodePath(linkDir))).toBe(false)
  })

  it('findNewestSessionId follows symlinks to find JSONLs under the real path', async () => {
    // Seed a .jsonl file into the project dir that the REAL path encodes to.
    // This is the directory Claude CLI would actually write into.
    const { CLAUDE_PROJECTS_DIR } = await import('../paths.js')
    const trueReal = await realpath(realDir)
    const projectDir = join(CLAUDE_PROJECTS_DIR, encodePath(trueReal))
    const { mkdir } = await import('node:fs/promises')
    await mkdir(projectDir, { recursive: true })
    const sessionId = `test-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
    const jsonlPath = join(projectDir, `${sessionId}.jsonl`)
    await writeFile(jsonlPath, '{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}\n')

    try {
      // Query using the SYMLINK path. Without realpath resolution this
      // would return null (looking under the wrong encoded dir).
      const found = await findNewestSessionId(linkDir, null)
      expect(found).toBe(sessionId)
    }
    finally {
      await rm(projectDir, { recursive: true, force: true })
    }
  })

  it('falls back to raw path when realpath throws (nonexistent cwd)', async () => {
    const ghost = '/definitely/not/a/real/path/abc123'
    const resolved = await resolvedProjectDir(ghost)
    expect(resolved.endsWith(encodePath(ghost))).toBe(true)
  })
})

describe('extractJsonBlock', () => {
  it('parses a fenced json block', () => {
    const text = 'Some prose.\n\n```json\n{"foo": 1, "bar": "x"}\n```\n\nMore prose.'
    expect(extractJsonBlock(text)).toEqual({ foo: 1, bar: 'x' })
  })

  it('returns null when no json block exists', () => {
    expect(extractJsonBlock('just text')).toBeNull()
  })

  it('returns null when the block contains invalid json', () => {
    const text = '```json\n{not valid}\n```'
    expect(extractJsonBlock(text)).toBeNull()
  })

  it('returns null when the json parses to a non-object (array, number)', () => {
    expect(extractJsonBlock('```json\n[1,2,3]\n```')).toBeNull()
    expect(extractJsonBlock('```json\n42\n```')).toBeNull()
  })

  it('extracts the last JSON block when multiple are present', () => {
    const text = '```json\n{"a": 1}\n```\n```json\n{"b": 2}\n```'
    expect(extractJsonBlock(text)).toEqual({ b: 2 })
  })
})

describe('lastAssistantText', () => {
  it('concatenates text blocks from the last assistant turn', () => {
    const entries = [
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'q' }] } },
      {
        type: 'assistant',
        message: {
          role: 'assistant',
          content: [
            { type: 'text', text: 'part 1' },
            { type: 'tool_use', text: undefined },
            { type: 'text', text: 'part 2' },
          ],
        },
      },
    ]
    expect(lastAssistantText(entries)).toBe('part 1\npart 2')
  })

  it('walks backward past tool-only assistant turns to find the last text turn', () => {
    const entries = [
      {
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'text', text: 'earlier answer' }] },
      },
      {
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'tool_use' }] },
      },
    ]
    // lastAssistantText returns the text of the most recent assistant turn
    // WITH text content, which is the second-to-last entry here.
    expect(lastAssistantText(entries)).toBe('earlier answer')
  })

  it('returns null when no assistant turn has text', () => {
    const entries = [
      { type: 'user', message: { role: 'user', content: [{ type: 'text', text: 'q' }] } },
    ]
    expect(lastAssistantText(entries)).toBeNull()
  })
})
