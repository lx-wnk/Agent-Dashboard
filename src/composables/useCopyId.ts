import type { MaybeRefOrGetter } from 'vue'
import { useClipboard } from '@vueuse/core'
import { toValue } from 'vue'

export function shortId(id: string): string {
  return id.slice(0, 8)
}

export function useCopyId(id: MaybeRefOrGetter<string>) {
  const { copy, copied } = useClipboard({ legacy: true })
  return { copy: () => copy(toValue(id)), copied }
}
