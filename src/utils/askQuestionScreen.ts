export interface DetectedOption {
  index: number
  label: string
  description?: string
}

/**
 * A parsed AskUserQuestion modal.
 *
 * Invariant (ENFORCED, not merely typical): the real option rows are numbered
 * contiguously `1..n`, and the two UI-injected meta-rows follow immediately, so
 * `typeSomethingIndex === options.length + 1` and
 * `chatAboutIndex === options.length + 2`. Any frame that violates this yields
 * `null` from `detectQuestion` rather than a desynced result.
 */
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

interface NumberedEntry {
  row: ParsedRow & { num: number, label: string }
  idx: number
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
 * PRIMARY signal: a real multi-select renders a checkbox on EVERY option row.
 * Only when there is zero checkbox evidence do we fall back to the true footer
 * (lines after the last numbered row) for a "toggle"/"space to" hint. Header,
 * question, and description lines are never scanned for it — that would let a
 * question like "Would you like to toggle X?" flip the mode.
 */
function decideMultiSelect(optionEntries: NumberedEntry[], contentLines: ParsedRow[]): boolean {
  const anyCheckbox = optionEntries.some(e => e.row.hasCheckbox)
  if (anyCheckbox)
    return optionEntries.every(e => e.row.hasCheckbox)

  const lastNumberedIdx = contentLines.reduce((max, row, idx) => row.num !== undefined ? idx : max, -1)
  return contentLines
    .slice(lastNumberedIdx + 1)
    .some(l => l.num === undefined && TOGGLE_HINT_RE.test(l.text))
}

/**
 * Detects an AskUserQuestion modal from the visible rows of a terminal buffer.
 *
 * Keys off structural signals present in the real TUI render (see memory:
 * lesson_askuserquestion_tui_keys) rather than exact spacing/border glyphs:
 * a contiguous numbered option block followed by BOTH UI-injected meta-rows.
 * Requiring both meta-rows — adjacent and index-continuous — is what separates a
 * real modal from an ordinary numbered list in terminal output. End-to-end
 * validation against a real pty render happens in Task 15.
 */
export function detectQuestion(rows: string[]): DetectedQuestion | null {
  const contentLines = rows
    .map(toContentLine)
    .filter((line): line is string => line !== null)
    .map(parseNumberedRow)

  const numbered: NumberedEntry[] = contentLines
    .map((row, idx) => ({ row, idx }))
    .filter((e): e is NumberedEntry => e.row.num !== undefined && e.row.label !== undefined)

  // The detector is coupled to the exact TUI copy (v2.1.197): a wording change
  // ("Type something…", "Chat about this (Tab)") would silently disable detection.
  // This coupling is the contract validated end-to-end in Task 15.
  const typeRow = numbered.find(e => e.row.label.toLowerCase() === TYPE_SOMETHING_LABEL)
  const chatRow = numbered.find(e => e.row.label.toLowerCase() === CHAT_ABOUT_LABEL)
  if (!typeRow || !chatRow)
    return null

  const typeSomethingIndex = typeRow.row.num
  const chatAboutIndex = chatRow.row.num

  const optionEntries = numbered.filter(e =>
    e !== typeRow && e !== chatRow && e.row.num < typeSomethingIndex)

  // ENFORCED invariant: real options numbered contiguously 1..n, meta-rows at n+1 / n+2.
  // A numbered-looking description line desyncs this — reject rather than emit garbage.
  const contiguous = optionEntries.every((e, i) => e.row.num === i + 1)
  const gateOk = optionEntries.length >= 1
    && contiguous
    && typeSomethingIndex === optionEntries.length + 1
    && chatAboutIndex === typeSomethingIndex + 1
  if (!gateOk)
    return null

  const options: DetectedOption[] = optionEntries.map((e) => {
    const option: DetectedOption = { index: e.row.num, label: e.row.label }
    const next = contentLines[e.idx + 1]
    if (next && next.num === undefined)
      option.description = next.text
    return option
  })

  const preamble = contentLines.slice(0, optionEntries[0].idx).map(l => l.text)
  const header = preamble[0] ?? ''
  const question = [...preamble].reverse().find(l => l.endsWith('?')) ?? preamble[preamble.length - 1] ?? header

  return {
    header,
    question,
    multiSelect: decideMultiSelect(optionEntries, contentLines),
    options,
    typeSomethingIndex,
    chatAboutIndex,
  }
}
