import { describe, expect, it } from 'vitest'
import { encodePath, extractSessionInfo, parseJsonlLines } from './jsonlParser'

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
