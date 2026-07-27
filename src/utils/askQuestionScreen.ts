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

/**
 * The AskUserQuestion review/submit screen — the screen a multi-question flow
 * lands on once every question is answered. It carries no meta-rows, so it is
 * not a `DetectedQuestion`: there is nothing to type and nothing to chat about,
 * only two numbered options to pick from.
 */
export interface DetectedConfirm {
  question: string
  options: DetectedOption[]
}

const TYPE_SOMETHING_LABEL = 'type something'
const CHAT_ABOUT_LABEL = 'chat about this'
const SUBMIT_LABEL = 'submit'
const CANCEL_LABEL = 'cancel'

const TRAILING_PUNCT_RE = /[\s.]+$/

// The meta-row copy drifts between Claude Code releases (e.g. v2.1.205 renders
// "Type something." with a trailing period, v2.1.197 rendered "type something").
// Match on a normalized prefix — lower-cased, trailing punctuation stripped — so
// a cosmetic copy tweak does not silently disable question detection.
function normalizeLabel(label: string): string {
  return label.toLowerCase().replace(TRAILING_PUNCT_RE, '')
}

function metaLabelMatches(label: string, meta: string): boolean {
  return normalizeLabel(label).startsWith(meta)
}

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
 * Reduces raw terminal rows to the content lines that carry meaning (borders
 * and blank rows dropped), each already parsed for a leading `N.` row number,
 * label and checkbox.
 */
function parseRows(rows: string[]): ParsedRow[] {
  return rows
    .map(toContentLine)
    .filter((line): line is string => line !== null)
    .map(parseNumberedRow)
}

function numberedEntries(contentLines: ParsedRow[]): NumberedEntry[] {
  return contentLines
    .map((row, idx) => ({ row, idx }))
    .filter((e): e is NumberedEntry => e.row.num !== undefined && e.row.label !== undefined)
}

/**
 * Picks the prompt line: the LAST preamble line ending in `?` (scanning
 * backwards, so a recap of earlier questions above the real prompt does not
 * win), falling back to the last preamble line.
 */
function questionFromPreamble(preamble: string[]): string {
  return [...preamble].reverse().find(l => l.endsWith('?')) ?? preamble[preamble.length - 1] ?? ''
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
 * Keys off structural signals in the real TUI render rather than exact
 * spacing/border glyphs: a contiguous numbered option block followed by BOTH
 * UI-injected meta-rows. Requiring both meta-rows — adjacent and
 * index-continuous — is what separates a real modal from an ordinary numbered
 * list in terminal output.
 */
export function detectQuestion(rows: string[]): DetectedQuestion | null {
  const contentLines = parseRows(rows)
  const numbered = numberedEntries(contentLines)

  // The detector keys off the meta-row copy, which drifts between Claude Code
  // releases; metaLabelMatches normalizes for trailing punctuation and suffixes
  // so a cosmetic tweak (e.g. "Type something." in v2.1.205) does not silently
  // disable detection. A wholesale rename would still require an update here.
  const typeRow = numbered.find(e => metaLabelMatches(e.row.label, TYPE_SOMETHING_LABEL))
  const chatRow = numbered.find(e => metaLabelMatches(e.row.label, CHAT_ABOUT_LABEL))
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
  const question = questionFromPreamble(preamble) || header

  return {
    header,
    question,
    multiSelect: decideMultiSelect(optionEntries, contentLines),
    options,
    typeSomethingIndex,
    chatAboutIndex,
  }
}

/**
 * Detects the AskUserQuestion review/submit screen:
 *
 * ```
 * Review your answers
 *   ... recap of the given answers ...
 * Ready to submit your answers?
 * ❯ 1. Submit answers
 *   2. Cancel
 * ```
 *
 * It has NO meta-rows, so `detectQuestion` rejects it by design; without this
 * second detector the dashboard goes blind exactly when the flow needs one last
 * keypress, and a multi-question round can never be completed from the UI.
 *
 * Gate: exactly two numbered rows, numbered 1 and 2, labelled Submit/Cancel,
 * with no meta-row anywhere on screen. The Submit/Cancel label pair is the
 * signal — deliberately NOT the surrounding copy ("Review your answers",
 * "Ready to submit your answers?"), which drifts between Claude Code releases
 * and would silently disable detection again the next time it is reworded.
 */
export function detectConfirmScreen(rows: string[]): DetectedConfirm | null {
  const contentLines = parseRows(rows)
  const numbered = numberedEntries(contentLines)

  if (numbered.length !== 2)
    return null
  if (numbered.some(e => metaLabelMatches(e.row.label, TYPE_SOMETHING_LABEL) || metaLabelMatches(e.row.label, CHAT_ABOUT_LABEL)))
    return null

  const [submit, cancel] = numbered
  if (submit.row.num !== 1 || cancel.row.num !== 2)
    return null
  if (!normalizeLabel(submit.row.label).startsWith(SUBMIT_LABEL) || normalizeLabel(cancel.row.label) !== CANCEL_LABEL)
    return null

  const preamble = contentLines.slice(0, submit.idx).map(l => l.text)
  if (preamble.length === 0)
    return null

  return {
    question: questionFromPreamble(preamble),
    options: [
      { index: submit.row.num, label: submit.row.label },
      { index: cancel.row.num, label: cancel.row.label },
    ],
  }
}

/**
 * Identity of a detected screen by CONTENT, not object reference.
 *
 * Both detectors are stateless w.r.t. user input, so their output changes only
 * when the SCREEN changes — but every SSE frame and every poll tick hands the
 * UI a freshly deserialized object. Components key their local answer state
 * (selections, typed text) off this signature so an unchanged screen never
 * discards what the user has entered.
 */
export function screenSignature(screen: DetectedQuestion | DetectedConfirm | null): string | null {
  if (screen === null)
    return null
  return JSON.stringify([
    'header' in screen ? screen.header : '',
    screen.question,
    'multiSelect' in screen ? screen.multiSelect : null,
    screen.options.map(o => [o.index, o.label]),
  ])
}
