export interface DetectedOption {
  index: number
  label: string
  description?: string
}

export interface DetectedQuestion {
  header: string
  question: string
  multiSelect: boolean
  options: DetectedOption[]
  typeSomethingIndex: number
  chatAboutIndex: number
}

const TYPE_SOMETHING_LABEL = 'type something'
const CHAT_ABOUT_LABEL = 'chat about this'

// The box-drawing block (U+2500-U+257F) covers rounded corners, straight edges, and dashes.
// eslint-disable-next-line regexp/no-obscure-range -- intentional Unicode block range, see comment above
const BORDER_ONLY_RE = /^[\s─-╿=+-]*$/
const EDGE_BOX_CHARS = '│║┃┆┇┊┋'
const LEADING_BOX_RE = new RegExp(`^\\s*[${EDGE_BOX_CHARS}]`)
const TRAILING_BOX_RE = new RegExp(`[${EDGE_BOX_CHARS}]\\s*$`)
const NUMBERED_ROW_RE = /^❯?\s*(\d+)\.\s+(\S.*)$/
const CHECKBOX_RE = /^\[[ x✔✓]\]\s*/i
const TRAILING_CR_RE = /\r$/
const TOGGLE_HINT_RE = /toggle|space to/i

function toContentLine(rawRow: string): string | null {
  const row = rawRow.replace(TRAILING_CR_RE, '')
  if (BORDER_ONLY_RE.test(row))
    return null

  let inner = row
  const leading = inner.match(LEADING_BOX_RE)
  if (leading)
    inner = inner.slice(leading[0].length)
  const trailing = inner.match(TRAILING_BOX_RE)
  if (trailing)
    inner = inner.slice(0, inner.length - trailing[0].length)

  const trimmed = inner.trim()
  return trimmed.length > 0 ? trimmed : null
}

interface ParsedRow {
  text: string
  num?: number
  label?: string
  hasCheckbox?: boolean
}

function parseNumberedRow(text: string): ParsedRow {
  const match = text.match(NUMBERED_ROW_RE)
  if (!match)
    return { text }

  const num = Number(match[1])
  const remainder = match[2]
  const checkbox = remainder.match(CHECKBOX_RE)
  const label = checkbox ? remainder.slice(checkbox[0].length).trim() : remainder.trim()
  return { text, num, label, hasCheckbox: Boolean(checkbox) }
}

/**
 * Detects an AskUserQuestion modal from the visible rows of a terminal buffer.
 *
 * Fixture-independent by design: it keys off two structural signals present in the real
 * TUI render — numbered option rows and the UI-injected "Type something" meta-row (see
 * memory: lesson_askuserquestion_tui_keys) — rather than exact spacing/border glyphs.
 * End-to-end validation against a real pty render happens in Task 15.
 */
export function detectQuestion(rows: string[]): DetectedQuestion | null {
  const contentLines = rows
    .map(toContentLine)
    .filter((line): line is string => line !== null)
    .map(parseNumberedRow)

  const firstOptionIdx = contentLines.findIndex(l => l.num !== undefined)
  if (firstOptionIdx === -1)
    return null

  const preamble = contentLines.slice(0, firstOptionIdx).map(l => l.text)
  const header = preamble[0] ?? ''
  const question = [...preamble].reverse().find(l => l.endsWith('?')) ?? preamble[preamble.length - 1] ?? header

  let typeSomethingIndex: number | undefined
  let chatAboutIndex: number | undefined
  let hasCheckbox = false
  const options: DetectedOption[] = []

  for (let i = firstOptionIdx; i < contentLines.length; i++) {
    const row = contentLines[i]
    if (row.num === undefined || row.label === undefined)
      continue

    const normalizedLabel = row.label.toLowerCase()
    if (normalizedLabel === TYPE_SOMETHING_LABEL) {
      typeSomethingIndex = row.num
      continue
    }
    if (normalizedLabel === CHAT_ABOUT_LABEL) {
      chatAboutIndex = row.num
      continue
    }

    if (row.hasCheckbox)
      hasCheckbox = true

    const option: DetectedOption = { index: row.num, label: row.label }
    const next = contentLines[i + 1]
    if (next && next.num === undefined)
      option.description = next.text

    options.push(option)
  }

  if (typeSomethingIndex === undefined || options.length === 0)
    return null

  const footerSaysToggle = contentLines.some(l => l.num === undefined && TOGGLE_HINT_RE.test(l.text))

  return {
    header,
    question,
    multiSelect: hasCheckbox || footerSaysToggle,
    options,
    typeSomethingIndex,
    chatAboutIndex: chatAboutIndex ?? typeSomethingIndex + 1,
  }
}
