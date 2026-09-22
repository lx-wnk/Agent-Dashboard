const MAX_429_RETRIES = 3
const DEFAULT_RETRY_AFTER_MS = 1000
const MAX_RETRY_AFTER_MS = 5000

function retryDelayMs(res: Response): number {
  const seconds = Number.parseInt(res.headers.get('Retry-After') ?? '', 10)
  const ms = Number.isFinite(seconds) && seconds >= 0 ? seconds * 1000 : DEFAULT_RETRY_AFTER_MS
  return Math.min(ms, MAX_RETRY_AFTER_MS)
}

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms))
}

// The dashboard's own boot burst can exhaust the shared per-IP rate limiter
// (server/internal/api/middleware.go); a 429 here is transient load, not a
// real failure, so both the load and the save get a few retries before
// reporting failure.
export async function fetchWithRateLimitRetry(input: string, init?: RequestInit): Promise<Response> {
  let res = await fetch(input, init)
  for (let attempt = 0; attempt < MAX_429_RETRIES && res.status === 429; attempt++) {
    await sleep(retryDelayMs(res))
    res = await fetch(input, init)
  }
  return res
}
