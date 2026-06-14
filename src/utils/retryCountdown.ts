export function secondsUntil(isoTimestamp: string | null | undefined): number {
  if (!isoTimestamp)
    return 0
  return Math.max(0, Math.round((new Date(isoTimestamp).getTime() - Date.now()) / 1000))
}
