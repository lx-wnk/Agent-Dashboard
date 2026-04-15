import { cpus, freemem } from 'node:os'

export interface ResourceRecommendation {
  recommended: number
  reason: string
  details: {
    cpuCount: number
    freeRamGb: number
    ramRecommended: number
    cpuRecommended: number
  }
}

/**
 * Recommend a maxParallelOrchestrators value based on current system resources.
 * Rule: min(floor(freeRamGB / 4), cpuCount / 2, 5), with a minimum of 1.
 *
 * Rationale: Each Opus orchestrator plus its subagents needs ~4 GB peak RAM
 * and should leave CPU headroom for other work. Cap at 5 to avoid thrashing.
 */
export function recommendParallelism(): ResourceRecommendation {
  const cpuCount = cpus().length
  const freeRamGb = freemem() / (1024 ** 3)
  const ramRecommended = Math.floor(freeRamGb / 4)
  const cpuRecommended = Math.floor(cpuCount / 2)
  const raw = Math.min(ramRecommended, cpuRecommended, 5)
  const recommended = Math.max(1, raw)

  let reason: string
  if (ramRecommended < cpuRecommended && ramRecommended < 5)
    reason = `RAM-limited: ${freeRamGb.toFixed(1)} GB free (÷ 4 GB per orchestrator)`
  else if (cpuRecommended < 5)
    reason = `CPU-limited: ${cpuCount} cores (÷ 2 for headroom)`
  else
    reason = `Capped at 5 to avoid thrashing`

  return {
    recommended,
    reason,
    details: {
      cpuCount,
      freeRamGb: Number(freeRamGb.toFixed(1)),
      ramRecommended,
      cpuRecommended,
    },
  }
}
