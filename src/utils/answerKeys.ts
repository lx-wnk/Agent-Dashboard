export type AnswerIntent
  = | { mode: 'single', index: number }
    | { mode: 'multi', indices: number[] }
    | { mode: 'custom', optionCount: number, text: string }
    | { mode: 'chat', optionCount: number, text: string }

export function encodeAnswer(intent: AnswerIntent): string[] {
  switch (intent.mode) {
    case 'single':
      return [String(intent.index + 1)]
    case 'multi':
      return [...intent.indices.map(i => String(i + 1)), '\t', '\r']
    case 'custom':
      return [String(intent.optionCount + 1), intent.text, '\r']
    case 'chat':
      return [String(intent.optionCount + 2), intent.text, '\r']
  }
}
