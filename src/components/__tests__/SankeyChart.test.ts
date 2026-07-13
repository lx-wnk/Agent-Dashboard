import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let SankeyChart: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  SankeyChart = (await import('@/features/workflows/components/visualizations/SankeyChart.vue')).default
})

describe('sankeyChart', () => {
  it('calls toast.error when error prop is set and renders no inline danger text', async () => {
    const w = mount(SankeyChart, { props: { data: null, loading: false, error: 'fetch failed' } })
    await nextTick()
    expect(toastMod.toast.error).toHaveBeenCalledWith('fetch failed')
    expect(w.find('.text-danger-text').exists()).toBe(false)
  })
})
