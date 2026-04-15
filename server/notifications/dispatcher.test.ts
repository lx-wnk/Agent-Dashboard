import type { NotificationAdapter } from './types.js'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import process from 'node:process'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { closeDb, getDb } from '../db/client.js'
import { setPreference } from '../db/notificationConfigRepo.js'
import { createDispatcher } from './dispatcher.js'

let tmpDir: string

beforeEach(() => {
  tmpDir = mkdtempSync(join(tmpdir(), 'dispatcher-test-'))
  process.env.DASHBOARD_DB_PATH = join(tmpDir, 'test.db')
  getDb()
})

afterEach(() => {
  closeDb()
  rmSync(tmpDir, { recursive: true, force: true })
  delete process.env.DASHBOARD_DB_PATH
})

function mockAdapter(channel: NotificationAdapter['channel'], configured = true): NotificationAdapter & { calls: number } {
  const adapter = {
    channel,
    calls: 0,
    isConfigured() { return configured },
    async send() {
      (adapter as { calls: number }).calls++
    },
  } as NotificationAdapter & { calls: number }
  return adapter
}

describe('createDispatcher', () => {
  it('returns empty array when preference is missing or disabled', async () => {
    const d = createDispatcher()
    const res1 = await d.dispatch({
      eventType: 'on_hold',
      title: 'x',
      body: 'y',
      taskId: 't',
      taskSlug: 's',
    })
    expect(res1).toEqual([])

    setPreference('on_hold', ['email'], false)
    const res2 = await d.dispatch({
      eventType: 'on_hold',
      title: 'x',
      body: 'y',
      taskId: 't',
      taskSlug: 's',
    })
    expect(res2).toEqual([])
  })

  it('dispatches only to channels in preference', async () => {
    setPreference('on_hold', ['email', 'browser'], true)

    const email = mockAdapter('email')
    const webhook = mockAdapter('webhook')
    const browser = mockAdapter('browser')
    const system = mockAdapter('system')

    const d = createDispatcher({ email, webhook, browser, system })
    const res = await d.dispatch({
      eventType: 'on_hold',
      title: 'Test',
      body: 'Body',
      taskId: 'tid',
      taskSlug: 'slug',
    })

    expect(email.calls).toBe(1)
    expect(browser.calls).toBe(1)
    expect(webhook.calls).toBe(0)
    expect(system.calls).toBe(0)
    expect(res.every(r => r.ok)).toBe(true)
    expect(res.map(r => r.channel).sort()).toEqual(['browser', 'email'])
  })

  it('marks unconfigured adapters as failed without throwing', async () => {
    setPreference('completed', ['email'], true)

    const email = mockAdapter('email', false)
    const d = createDispatcher({ email })
    const res = await d.dispatch({
      eventType: 'completed',
      title: 'x',
      body: 'y',
      taskId: 't',
      taskSlug: 's',
    })

    expect(email.calls).toBe(0)
    expect(res[0].ok).toBe(false)
    expect(res[0].error).toContain('not configured')
  })

  it('catches adapter errors and continues to other channels', async () => {
    setPreference('failed', ['email', 'webhook'], true)

    const email: NotificationAdapter = {
      channel: 'email',
      isConfigured: () => true,
      async send() { throw new Error('SMTP down') },
    }
    const webhook = mockAdapter('webhook')

    const d = createDispatcher({ email, webhook })
    const res = await d.dispatch({
      eventType: 'failed',
      title: 'x',
      body: 'y',
      taskId: 't',
      taskSlug: 's',
    })

    expect(res).toHaveLength(2)
    const emailResult = res.find(r => r.channel === 'email')
    const webhookResult = res.find(r => r.channel === 'webhook')
    expect(emailResult?.ok).toBe(false)
    expect(emailResult?.error).toContain('SMTP down')
    expect(webhookResult?.ok).toBe(true)
    expect(webhook.calls).toBe(1)
  })
})
