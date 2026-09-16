export const AVAILABLE_MODELS = [
  'claude-fable-5-1',
  'claude-opus-5',
  'claude-sonnet-5',
  'claude-opus-4-8',
  'claude-opus-4-6',
  'claude-sonnet-4-6',
  'claude-haiku-4-5',
] as const

export type AvailableModel = typeof AVAILABLE_MODELS[number]

export type ModelSeries = 'opus' | 'sonnet' | 'haiku' | 'fable'

// Kept in parity by hand with claudemodel.Latest (server/internal/claudemodel):
// version parts compare as numbers, and a shorter version sorts first when it
// is a prefix of the longer one, so '5' < '5-1' and '4-8' < '4-10'.
export function compareModelVersions(a: string, b: string): number {
  const pa = a.split('-').map(Number)
  const pb = b.split('-').map(Number)
  for (let i = 0; i < Math.min(pa.length, pb.length); i++) {
    if (pa[i] !== pb[i])
      return pa[i] - pb[i]
  }
  return pa.length - pb.length
}

/** The newest model of a series in AVAILABLE_MODELS — the default wherever a model is suggested. */
export function latestModel(series: ModelSeries): AvailableModel {
  const prefix = `claude-${series}-`
  let latest: AvailableModel | undefined
  for (const id of AVAILABLE_MODELS) {
    if (!id.startsWith(prefix))
      continue
    if (!latest || compareModelVersions(id.slice(prefix.length), latest.slice(prefix.length)) > 0)
      latest = id
  }
  if (!latest)
    throw new Error(`no known model in series ${series}`)
  return latest
}

// Reasoning-effort levels for the claude adapter's adapter_config.effort key
// (server/internal/services/effort_resolver.go). The server stores this as an
// arbitrary string — no Go-side enum exists to derive from — so this list is
// hand-kept in parity with what the claude CLI actually accepts.
// Kept in parity by hand with services.ValidEffortLevels — the Vue client
// cannot import Go. The set is what `claude --effort` accepts; offering fewer
// here would make a value set through the API render as unrecognised.
export const EFFORT_OPTIONS = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
  { value: 'xhigh', label: 'Extra high' },
  { value: 'max', label: 'Max' },
] as const

export type EffortLevel = typeof EFFORT_OPTIONS[number]['value']
