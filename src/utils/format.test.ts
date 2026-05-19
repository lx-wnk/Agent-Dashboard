import type { TokenUsage } from '../types'
import { describe, expect, it } from 'vitest'
import { maskToken, formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from './format'

describe('totalTokenCount', () => {
  it('sums all four token fields', () => {
    const usage: TokenUsage = {
      inputTokens: 100,
      outputTokens: 200,
      cacheReadTokens: 50,
      cacheCreationTokens: 25,
    }
    expect(totalTokenCount(usage)).toBe(375)
  })

  it('returns 0 when all fields are zero', () => {
    const usage: TokenUsage = {
      inputTokens: 0,
      outputTokens: 0,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    }
    expect(totalTokenCount(usage)).toBe(0)
  })

  it('handles mixed zeros and positive values', () => {
    const usage: TokenUsage = {
      inputTokens: 1000,
      outputTokens: 0,
      cacheReadTokens: 0,
      cacheCreationTokens: 500,
    }
    expect(totalTokenCount(usage)).toBe(1500)
  })

  it('handles large token counts without overflow', () => {
    const usage: TokenUsage = {
      inputTokens: 1_000_000,
      outputTokens: 2_000_000,
      cacheReadTokens: 500_000,
      cacheCreationTokens: 250_000,
    }
    expect(totalTokenCount(usage)).toBe(3_750_000)
  })
})

describe('formatTokens', () => {
  it('returns em-dash for 0', () => {
    expect(formatTokens(0)).toBe('—')
  })

  it('returns the number as a string for values below 1000', () => {
    expect(formatTokens(1)).toBe('1')
    expect(formatTokens(999)).toBe('999')
    expect(formatTokens(500)).toBe('500')
  })

  it('formats thousands with one decimal and k suffix', () => {
    expect(formatTokens(1000)).toBe('1.0k')
    expect(formatTokens(1500)).toBe('1.5k')
    expect(formatTokens(999_999)).toBe('1000.0k')
  })

  it('formats millions with two decimals and M suffix', () => {
    expect(formatTokens(1_000_000)).toBe('1.00M')
    expect(formatTokens(2_500_000)).toBe('2.50M')
    expect(formatTokens(10_750_000)).toBe('10.75M')
  })

  it('boundary: 999 stays as string, 1000 gets k suffix', () => {
    expect(formatTokens(999)).toBe('999')
    expect(formatTokens(1000)).toBe('1.0k')
  })
})

describe('formatCost', () => {
  it('returns em-dash for 0', () => {
    expect(formatCost(0)).toBe('—')
  })

  it('returns <$0.01 for very small positive costs', () => {
    expect(formatCost(0.001)).toBe('<$0.01')
    expect(formatCost(0.009)).toBe('<$0.01')
    // boundary: exactly 0.01 should NOT trigger the small-cost branch
    expect(formatCost(0.01)).toBe('$0.01')
  })

  it('formats normal costs with dollar sign and two decimals', () => {
    expect(formatCost(1)).toBe('$1.00')
    expect(formatCost(0.5)).toBe('$0.50')
    expect(formatCost(12.345)).toBe('$12.35')
  })

  it('formats large costs correctly', () => {
    expect(formatCost(100)).toBe('$100.00')
  })
})

describe('formatUptime', () => {
  it('shows seconds only when under 60s', () => {
    expect(formatUptime(0)).toBe('0s')
    expect(formatUptime(1)).toBe('1s')
    expect(formatUptime(59)).toBe('59s')
  })

  it('shows minutes only when under 1 hour', () => {
    expect(formatUptime(60)).toBe('1m')
    expect(formatUptime(90)).toBe('1m')
    expect(formatUptime(3599)).toBe('59m')
  })

  it('shows hours and minutes when under 24 hours', () => {
    expect(formatUptime(3600)).toBe('1h 0m')
    expect(formatUptime(3660)).toBe('1h 1m')
    expect(formatUptime(7384)).toBe('2h 3m')
    expect(formatUptime(86399)).toBe('23h 59m')
  })

  it('shows days and hours for 24 hours and above', () => {
    expect(formatUptime(86400)).toBe('1d 0h')
    expect(formatUptime(90000)).toBe('1d 1h')
    expect(formatUptime(172800)).toBe('2d 0h')
    expect(formatUptime(176400)).toBe('2d 1h')
  })

  it('boundary: 3600s shows hours, 3599s shows minutes', () => {
    expect(formatUptime(3599)).toBe('59m')
    expect(formatUptime(3600)).toBe('1h 0m')
  })
})

describe('shortModel', () => {
  it('returns em-dash for null', () => {
    expect(shortModel(null)).toBe('—')
  })

  it('returns em-dash for empty string', () => {
    expect(shortModel('')).toBe('—')
  })

  it('strips the claude- prefix and converts the trailing digit segment to a space-prefixed version', () => {
    // /-\d+$/ also matches the trailing single-segment digit e.g. "claude-opus-4" -> "opus 4"
    expect(shortModel('claude-opus-4')).toBe('opus 4')
    expect(shortModel('claude-haiku-3')).toBe('haiku 3')
  })

  it('replaces trailing date with a space-prefixed version', () => {
    // "-20250514" becomes " 20250514"
    expect(shortModel('claude-sonnet-4-20250514')).toBe('sonnet-4 20250514')
  })

  it('replaces any trailing digit segment (not just long dates)', () => {
    // "claude-sonnet-4-6": trailing "-6" becomes " 6", so result is "sonnet-4 6"
    expect(shortModel('claude-sonnet-4-6')).toBe('sonnet-4 6')
  })

  it('handles model names that do not start with claude-', () => {
    expect(shortModel('gpt-4o')).toBe('gpt-4o')
  })

  it('does not strip mid-string digits — only a single trailing number segment', () => {
    // "claude-opus-4-6" — the trailing "-6" is a version digit so it becomes " 6"
    expect(shortModel('claude-opus-4-6')).toBe('opus-4 6')
  })
})

describe('maskToken', () => {
  it('masks middle of token keeping first 8 and last 4 chars', () => {
    const token = 'mcp_abcdefghij1234'
    // length=18: first 8 = 'mcp_abcd', last 4 = '1234', middle = max(8, 18-12) = 8 bullets
    expect(maskToken(token)).toBe('mcp_abcd••••••••1234')
  })

  it('uses at least 8 bullets for short tokens', () => {
    const token = 'mcp_1234'
    // length=8: first 8 already covers all, pad 8 bullets minimum
    expect(maskToken(token)).toBe('mcp_1234••••••••1234')
  })

  it('handles a realistic 40-char MCP token', () => {
    const token = 'mcp_' + 'a'.repeat(36)
    // length=40: first 8 = 'mcp_aaaa', last 4 = 'aaaa', middle = 28 bullets
    expect(maskToken(token)).toBe('mcp_aaaa' + '•'.repeat(28) + 'aaaa')
  })

  it('never reveals more than first 8 + last 4 chars', () => {
    const token = 'mcp_' + 'x'.repeat(100)
    const masked = maskToken(token)
    expect(masked.startsWith('mcp_')).toBe(true)
    expect(masked.endsWith('xxxx')).toBe(true)
    expect(masked).toContain('•')
    const visible = masked.replace(/•/g, '')
    expect(visible).toBe(token.slice(0, 8) + token.slice(-4))
  })
})
