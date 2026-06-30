export interface TrackerIssue {
  tracker: string
  key: string
  title: string
  body: string
  url: string
  labels: string[]
}

export function useTrackerImport() {
  async function fetchIssue(ref: string): Promise<TrackerIssue> {
    const res = await fetch('/api/tracker/fetch', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Origin': globalThis.location.origin,
      },
      body: JSON.stringify({ ref }),
    })
    if (!res.ok) {
      let detail = `HTTP ${res.status}`
      try {
        const b = await res.json()
        if (b?.error)
          detail = b.error
      }
      catch { /* no body */ }
      throw new Error(detail)
    }
    return res.json()
  }

  return { fetchIssue }
}
