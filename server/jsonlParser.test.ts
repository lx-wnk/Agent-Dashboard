import { mkdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { encodePath, extractSessionInfo, incrementalRead, mergeIncrementalInfo, parseJsonlLines, pickBestJsonlFile } from './jsonlParser'

describe('parseJsonlLines', () => {
  it('parses valid JSON lines into an array', () => {
    const raw = '{"type":"user"}\n{"type":"assistant"}'
    const result = parseJsonlLines(raw)
    expect(result).toEqual([{ type: 'user' }, { type: 'assistant' }])
  })

  it('skips broken/partial JSON lines without throwing', () => {
    const raw = 'broken json line\n{"type":"user"}'
    const result = parseJsonlLines(raw)
    expect(result).toEqual([{ type: 'user' }])
  })

  it('returns empty array for empty string', () => {
    expect(parseJsonlLines('')).toEqual([])
  })

  it('returns empty array for whitespace-only string', () => {
    expect(parseJsonlLines('   \n   \n  ')).toEqual([])
  })

  it('handles mixed valid and invalid lines', () => {
    const raw = [
      '{"a":1}',
      '{bad json',
      '{"b":2}',
      '   ',
      '{"c":3}',
    ].join('\n')
    const result = parseJsonlLines(raw)
    expect(result).toEqual([{ a: 1 }, { b: 2 }, { c: 3 }])
  })

  it('handles a single valid line without trailing newline', () => {
    expect(parseJsonlLines('{"x":42}')).toEqual([{ x: 42 }])
  })

  it('handles lines that are valid but empty JSON values', () => {
    const raw = '"hello"\n42\nnull'
    const result = parseJsonlLines(raw)
    expect(result).toEqual(['hello', 42, null])
  })
})

describe('encodePath', () => {
  it('converts forward slashes to dashes', () => {
    expect(encodePath('/Users/alex/project')).toBe('-Users-alex-project')
  })

  it('converts underscores to dashes', () => {
    expect(encodePath('/Users/alex/my_project')).toBe('-Users-alex-my-project')
  })

  it('converts both slashes and underscores', () => {
    expect(encodePath('/home/user_name/some_dir/project')).toBe('-home-user-name-some-dir-project')
  })

  it('handles path with no underscores', () => {
    expect(encodePath('/code/repo')).toBe('-code-repo')
  })

  it('handles path that is just root slash', () => {
    expect(encodePath('/')).toBe('-')
  })

  it('handles path with consecutive underscores', () => {
    expect(encodePath('/foo__bar')).toBe('-foo--bar')
  })

  it('converts dots to dashes (Claude CLI encoding)', () => {
    // Observed from ~/.claude/projects: `/.claude/` becomes `--claude-`
    // because both the leading slash AND the dot each map to -.
    expect(encodePath('/home/.claude/worktrees/x')).toBe('-home--claude-worktrees-x')
  })

  it('handles paths with dotted segments in the middle', () => {
    expect(encodePath('/Users/alex/dot-files/home/.claude/x')).toBe('-Users-alex-dot-files-home--claude-x')
  })
})

describe('extractSessionInfo', () => {
  it('returns zeroed token usage for empty entries', () => {
    const result = extractSessionInfo([])
    expect(result.tokenUsage).toEqual({
      inputTokens: 0,
      outputTokens: 0,
      cacheCreationTokens: 0,
      cacheReadTokens: 0,
    })
  })

  it('returns empty arrays and nulls for empty entries', () => {
    const result = extractSessionInfo([])
    expect(result.lastTools).toEqual([])
    expect(result.tasks).toEqual([])
    expect(result.model).toBeNull()
    expect(result.codeVersion).toBeNull()
    expect(result.currentAction).toBeNull()
    expect(result.conversationTurns).toBe(0)
    expect(result.sessionId).toBe('')
  })

  it('aggregates token usage across multiple assistant entries', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          model: 'claude-3-5-sonnet-20241022',
          usage: {
            input_tokens: 100,
            output_tokens: 50,
            cache_creation_input_tokens: 10,
            cache_read_input_tokens: 5,
          },
        },
      },
      {
        type: 'assistant',
        message: {
          model: 'claude-3-5-sonnet-20241022',
          usage: {
            input_tokens: 200,
            output_tokens: 80,
            cache_creation_input_tokens: 20,
            cache_read_input_tokens: 15,
          },
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.tokenUsage).toEqual({
      inputTokens: 300,
      outputTokens: 130,
      cacheCreationTokens: 30,
      cacheReadTokens: 20,
    })
  })

  it('extracts model from assistant message', () => {
    const entries = [
      {
        type: 'assistant',
        message: { model: 'claude-opus-4', usage: {} },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.model).toBe('claude-opus-4')
  })

  it('uses last seen model when multiple assistant entries have different models', () => {
    const entries = [
      { type: 'assistant', message: { model: 'model-a', usage: {} } },
      { type: 'assistant', message: { model: 'model-b', usage: {} } },
    ]
    const result = extractSessionInfo(entries)
    expect(result.model).toBe('model-b')
  })

  it('extracts codeVersion from entry.version field', () => {
    const entries = [{ version: '1.2.3', type: 'user' }]
    const result = extractSessionInfo(entries)
    expect(result.codeVersion).toBe('1.2.3')
  })

  it('extracts sessionId from entry.sessionId field', () => {
    const entries = [{ sessionId: 'abc-123', type: 'user' }]
    const result = extractSessionInfo(entries)
    expect(result.sessionId).toBe('abc-123')
  })

  it('counts user turns as conversationTurns', () => {
    const entries = [
      { type: 'user' },
      { type: 'assistant', message: { usage: {} } },
      { type: 'user' },
      { type: 'user' },
    ]
    const result = extractSessionInfo(entries)
    expect(result.conversationTurns).toBe(3)
  })

  it('extracts tool names from assistant content blocks', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            { type: 'tool_use', name: 'Bash', input: {} },
            { type: 'tool_use', name: 'Read', input: {} },
          ],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.lastTools).toContain('Bash')
    expect(result.lastTools).toContain('Read')
  })

  it('does not duplicate tool names in lastTools', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            { type: 'tool_use', name: 'Bash', input: {} },
            { type: 'tool_use', name: 'Bash', input: {} },
          ],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.lastTools?.filter(t => t === 'Bash')).toHaveLength(1)
  })

  it('counts tool usage in toolCounts', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            { type: 'tool_use', name: 'Bash', input: {} },
            { type: 'tool_use', name: 'Bash', input: {} },
            { type: 'tool_use', name: 'Read', input: {} },
          ],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.toolCounts?.Bash).toBe(2)
    expect(result.toolCounts?.Read).toBe(1)
  })

  it('sets currentAction with command suffix when tool has command input', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            { type: 'tool_use', name: 'Bash', input: { command: 'npm test' } },
          ],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.currentAction).toBe('Bash: npm test')
  })

  it('sets currentAction without suffix when tool has no command input', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            { type: 'tool_use', name: 'Read', input: { file_path: '/foo' } },
          ],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.currentAction).toBe('Read')
  })

  it('sets currentAction and lastOutput from text blocks', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            { type: 'text', text: 'Here is the result of my analysis.' },
          ],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.currentAction).toBe('Here is the result of my analysis.')
    expect(result.lastOutput).toBe('Here is the result of my analysis.')
  })

  it('truncates currentAction to 300 characters from text blocks', () => {
    const longText = 'x'.repeat(400)
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [{ type: 'text', text: longText }],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.currentAction).toHaveLength(300)
    expect(result.lastOutput).toHaveLength(400) // lastOutput capped at 500
  })

  it('creates a task from TaskCreate tool use', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            {
              type: 'tool_use',
              name: 'TaskCreate',
              id: 'task-1',
              input: { subject: 'Write tests' },
            },
          ],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.tasks).toHaveLength(1)
    expect(result.tasks?.[0]).toMatchObject({
      id: 'task-1',
      subject: 'Write tests',
      status: 'pending',
    })
  })

  it('updates a task status with TaskUpdate tool use', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            {
              type: 'tool_use',
              name: 'TaskCreate',
              id: 'task-1',
              input: { subject: 'Write tests' },
            },
          ],
        },
      },
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            {
              type: 'tool_use',
              name: 'TaskUpdate',
              input: { taskId: 'task-1', status: 'completed' },
            },
          ],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.tasks?.[0].status).toBe('completed')
  })

  it('falls back to index-based id for TaskCreate when block.id is missing', () => {
    const entries = [
      {
        type: 'assistant',
        message: {
          usage: {},
          content: [
            { type: 'tool_use', name: 'TaskCreate', input: { subject: 'Task A' } },
          ],
        },
      },
    ]
    const result = extractSessionInfo(entries)
    expect(result.tasks?.[0].id).toBe('1')
  })

  it('ignores assistant entries with no message.usage field', () => {
    const entries = [
      { type: 'assistant', message: { model: 'claude-opus-4' } },
    ]
    const result = extractSessionInfo(entries)
    expect(result.tokenUsage).toEqual({
      inputTokens: 0,
      outputTokens: 0,
      cacheCreationTokens: 0,
      cacheReadTokens: 0,
    })
  })

  it('recognises cli entrypoint', () => {
    const entries = [{ entrypoint: 'cli' }]
    expect(extractSessionInfo(entries).entrypoint).toBe('cli')
  })

  it('recognises desktop entrypoint', () => {
    const entries = [{ entrypoint: 'desktop' }]
    expect(extractSessionInfo(entries).entrypoint).toBe('desktop')
  })

  it('falls back to unknown for unrecognised entrypoint', () => {
    const entries = [{ entrypoint: 'web' }]
    expect(extractSessionInfo(entries).entrypoint).toBe('unknown')
  })
})

describe('pickBestJsonlFile', () => {
  const now = Date.now()

  function file(name: string, offsetMs: number, birthtimeOffset?: number) {
    const mtime = new Date(now - offsetMs)
    const birthtimeMs = birthtimeOffset !== undefined
      ? now - birthtimeOffset
      : 0 // 0 = birthtime not supported
    return { name, mtime, birthtimeMs }
  }

  it('returns the single file regardless of uptime', () => {
    const f = file('a.jsonl', 1000)
    expect(pickBestJsonlFile([f])).toBe(f)
    expect(pickBestJsonlFile([f], 60)).toBe(f)
  })

  it('returns newest by mtime when no uptime provided', () => {
    const older = file('old.jsonl', 60_000)
    const newer = file('new.jsonl', 5_000)
    expect(pickBestJsonlFile([older, newer])).toBe(newer)
  })

  it('matches by birthtime when uptime is provided and birthtime is supported', () => {
    // Process started ~10 min ago → should get the 10-min-old file
    const uptimeSecs = 600
    const old = file('old.jsonl', 5_000, 605_000) // birthtime 605s ago ≈ 10 min
    const newer = file('new.jsonl', 2_000, 120_000) // birthtime 120s ago ≈ 2 min
    expect(pickBestJsonlFile([old, newer], uptimeSecs)).toBe(old)
  })

  it('matches the newer file when process is young', () => {
    const uptimeSecs = 90
    const old = file('old.jsonl', 5_000, 600_000)
    const newer = file('new.jsonl', 2_000, 95_000) // birthtime 95s ago ≈ uptime
    expect(pickBestJsonlFile([old, newer], uptimeSecs)).toBe(newer)
  })

  it('falls back to newest by mtime when birthtime is zero for all files', () => {
    const older = file('old.jsonl', 60_000, undefined) // birthtimeMs = 0
    const newer = file('new.jsonl', 5_000, undefined) // birthtimeMs = 0
    expect(pickBestJsonlFile([older, newer], 30)).toBe(newer)
  })
})

describe('incrementalRead', () => {
  let dir: string

  beforeAll(async () => {
    dir = join(tmpdir(), `jsonl-incr-test-${Date.now()}`)
    await mkdir(dir, { recursive: true })
  })

  afterAll(async () => {
    await rm(dir, { recursive: true, force: true })
  })

  it('reads exactly the bytes after fromOffset', async () => {
    const filePath = join(dir, 'incr1.jsonl')
    const content = '{"type":"user"}\n{"type":"assistant"}\n'
    await writeFile(filePath, content, 'utf-8')
    const firstLineBytes = Buffer.byteLength('{"type":"user"}\n', 'utf-8')

    const result = await incrementalRead(filePath, firstLineBytes)

    expect(result.raw).toBe('{"type":"assistant"}\n')
    expect(result.endOffset).toBe(Buffer.byteLength(content, 'utf-8'))
  })

  it('returns empty raw and same offset when fromOffset equals file size', async () => {
    const filePath = join(dir, 'incr2.jsonl')
    const content = '{"type":"user"}\n'
    await writeFile(filePath, content, 'utf-8')
    const size = Buffer.byteLength(content, 'utf-8')

    const result = await incrementalRead(filePath, size)

    expect(result.raw).toBe('')
    expect(result.endOffset).toBe(size)
  })

  it('returns empty raw when fromOffset exceeds file size', async () => {
    const filePath = join(dir, 'incr3.jsonl')
    await writeFile(filePath, 'hello\n', 'utf-8')
    const size = Buffer.byteLength('hello\n', 'utf-8')

    const result = await incrementalRead(filePath, size + 100)

    expect(result.raw).toBe('')
    expect(result.endOffset).toBe(size + 100)
  })

  it('reads entire file when fromOffset is 0', async () => {
    const filePath = join(dir, 'incr4.jsonl')
    const content = '{"a":1}\n{"b":2}\n'
    await writeFile(filePath, content, 'utf-8')

    const result = await incrementalRead(filePath, 0)

    expect(result.raw).toBe(content)
    expect(result.endOffset).toBe(Buffer.byteLength(content, 'utf-8'))
  })
})

describe('mergeIncrementalInfo', () => {
  it('sums token usage fields from prev and next', () => {
    const prev = {
      tokenUsage: { inputTokens: 100, outputTokens: 50, cacheCreationTokens: 10, cacheReadTokens: 5 },
    }
    const next = {
      tokenUsage: { inputTokens: 200, outputTokens: 80, cacheCreationTokens: 20, cacheReadTokens: 15 },
    }
    const result = mergeIncrementalInfo(prev, next)

    expect(result.tokenUsage).toEqual({
      inputTokens: 300,
      outputTokens: 130,
      cacheCreationTokens: 30,
      cacheReadTokens: 20,
    })
  })

  it('sums conversationTurns', () => {
    const result = mergeIncrementalInfo({ conversationTurns: 3 }, { conversationTurns: 7 })
    expect(result.conversationTurns).toBe(10)
  })

  it('merges toolCounts by summing per-key values', () => {
    const result = mergeIncrementalInfo(
      { toolCounts: { Read: 2, Write: 1 } },
      { toolCounts: { Read: 3, Bash: 4 } },
    )
    expect(result.toolCounts).toEqual({ Read: 5, Write: 1, Bash: 4 })
  })

  it('updates status of existing task and appends new tasks', () => {
    const prev = {
      tasks: [
        { id: 't1', subject: 'alpha', status: 'pending' },
        { id: 't2', subject: 'beta', status: 'pending' },
      ],
    }
    const next = {
      tasks: [
        { id: 't1', subject: 'alpha', status: 'completed' },
        { id: 't3', subject: 'gamma', status: 'pending' },
      ],
    }
    const result = mergeIncrementalInfo(prev, next)

    expect(result.tasks).toHaveLength(3)
    expect(result.tasks!.find(t => t.id === 't1')!.status).toBe('completed')
    expect(result.tasks!.find(t => t.id === 't2')!.status).toBe('pending')
    expect(result.tasks!.find(t => t.id === 't3')!.status).toBe('pending')
  })

  it('uses next.lastOutput if non-null, else falls back to prev', () => {
    expect(mergeIncrementalInfo({ lastOutput: 'old' }, { lastOutput: 'new' }).lastOutput).toBe('new')
    expect(mergeIncrementalInfo({ lastOutput: 'old' }, { lastOutput: null }).lastOutput).toBe('old')
    expect(mergeIncrementalInfo({ lastOutput: null }, { lastOutput: null }).lastOutput).toBeNull()
  })

  it('prefers non-unknown next.model over prev', () => {
    expect(
      mergeIncrementalInfo({ model: 'claude-3-5-sonnet' }, { model: null }).model,
    ).toBe('claude-3-5-sonnet')
    expect(
      mergeIncrementalInfo({ model: 'claude-3-5-sonnet' }, { model: 'claude-3-7-sonnet' }).model,
    ).toBe('claude-3-7-sonnet')
  })

  it('handles both prev and next being empty objects without throwing', () => {
    const result = mergeIncrementalInfo({}, {})
    expect(result.tokenUsage).toEqual({
      inputTokens: 0,
      outputTokens: 0,
      cacheCreationTokens: 0,
      cacheReadTokens: 0,
    })
    expect(result.conversationTurns).toBe(0)
    expect(result.tasks).toEqual([])
  })
})

describe('extractSessionInfo — compaction detection', () => {
  function makeAssistantEntry(inputTokens: number, outputTokens: number) {
    return {
      type: 'assistant',
      message: {
        usage: { input_tokens: inputTokens, output_tokens: outputTokens },
        content: [],
      },
    }
  }

  it('accumulates tokens normally when no compaction occurs', () => {
    const entries = [
      makeAssistantEntry(100, 50),
      makeAssistantEntry(120, 60),
      makeAssistantEntry(140, 70),
    ]
    const result = extractSessionInfo(entries)
    expect(result.tokenUsage!.inputTokens).toBe(360)
    expect(result.tokenUsage!.outputTokens).toBe(180)
  })

  it('detects compaction when input_tokens drops by >= 80% and preserves baseline', () => {
    const entries = [
      makeAssistantEntry(500, 200),
      makeAssistantEntry(500, 200),
      // 90% drop from 1000 — compaction event
      makeAssistantEntry(100, 40),
      makeAssistantEntry(110, 45),
    ]
    const result = extractSessionInfo(entries)
    // Baseline 1000 input preserved; post-compaction 210 added
    expect(result.tokenUsage!.inputTokens).toBe(1210)
  })

  it('does not trigger compaction when drop is less than 80%', () => {
    const entries = [
      makeAssistantEntry(1000, 400),
      makeAssistantEntry(250, 100), // 75% drop — not a compaction
    ]
    const result = extractSessionInfo(entries)
    expect(result.tokenUsage!.inputTokens).toBe(1250)
  })

  it('handles multiple compaction events in sequence', () => {
    const entries = [
      makeAssistantEntry(1000, 400),
      makeAssistantEntry(50, 20),    // first compaction
      makeAssistantEntry(800, 320),
      makeAssistantEntry(40, 16),    // second compaction
      makeAssistantEntry(200, 80),
    ]
    const result = extractSessionInfo(entries)
    // Baseline = 1000 + 50 + 800 = 1850; post-compaction = 240
    expect(result.tokenUsage!.inputTokens).toBe(2090)
  })
})
