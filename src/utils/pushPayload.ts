export interface PushNotice {
  title: string
  body: string
  url: string
  tag: string
}

export function parsePushNotice(data: unknown): PushNotice | null {
  if (typeof data !== 'object' || data === null)
    return null
  const d = data as Record<string, unknown>
  if (typeof d.title !== 'string' || d.title === '')
    return null
  return {
    title: d.title,
    body: typeof d.body === 'string' ? d.body : '',
    url: typeof d.url === 'string' ? d.url : '/',
    tag: typeof d.tag === 'string' ? d.tag : '',
  }
}
