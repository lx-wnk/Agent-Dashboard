import { describe, expect, it, vi } from 'vitest'
import { triggerDownload } from './download'

describe('triggerDownload', () => {
  it('creates a hidden anchor, clicks it, and removes it', () => {
    const anchor = document.createElement('a')
    const clickSpy = vi.spyOn(anchor, 'click').mockImplementation(() => {})
    const removeSpy = vi.spyOn(anchor, 'remove')
    const createSpy = vi.spyOn(document, 'createElement').mockReturnValue(anchor)
    const appendSpy = vi.spyOn(document.body, 'appendChild')

    triggerDownload('/api/tasks/export?format=json', 'tasks.json')

    expect(createSpy).toHaveBeenCalledWith('a')
    expect(anchor.href).toContain('/api/tasks/export?format=json')
    expect(anchor.download).toBe('tasks.json')
    expect(appendSpy).toHaveBeenCalledWith(anchor)
    expect(clickSpy).toHaveBeenCalledTimes(1)
    expect(removeSpy).toHaveBeenCalledTimes(1)

    createSpy.mockRestore()
  })

  it('sets an empty download attribute when no filename is given', () => {
    const anchor = document.createElement('a')
    vi.spyOn(anchor, 'click').mockImplementation(() => {})
    const createSpy = vi.spyOn(document, 'createElement').mockReturnValue(anchor)

    triggerDownload('/api/tasks/export?format=csv')

    expect(anchor.download).toBe('')

    createSpy.mockRestore()
  })
})
