import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SkeletonCard from './SkeletonCard.vue'

describe('skeletonCard', () => {
  it('renders an aria-hidden shimmer with pulse', () => {
    const w = mount(SkeletonCard)
    const root = w.get('div')
    expect(root.attributes('aria-hidden')).toBe('true')
    expect(root.classes()).toContain('animate-pulse')
    expect(root.classes()).toContain('motion-reduce:animate-none')
  })
})
