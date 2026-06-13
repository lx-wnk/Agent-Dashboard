import { useClipboard } from '@vueuse/core'

export function shortId(id: string): string {
  return id.slice(0, 8)
}

export function useCopyId(id: string) {
  const { copy, copied } = useClipboard({ legacy: true })
  return { copy: () => copy(id), copied }
}
