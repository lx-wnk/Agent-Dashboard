import express from 'express'
import request from 'supertest'
import { describe, expect, it } from 'vitest'
import { createHooksRouter } from './hooksRoutes.js'

function makeApp() {
  const app = express()
  app.use(express.json())
  app.use('/api/hooks', createHooksRouter({ onEvent: () => {}, secret: '' }))
  return app
}

describe('editGate', () => {
  it('auto-accepts non-Edit tool', async () => {
    const app = makeApp()
    const res = await request(app).post('/api/hooks/pre-tool').send({
      sessionId: 'sess-1',
      toolName: 'Read',
      filePath: '/foo.ts',
      oldContent: '',
      newContent: 'x',
    })
    expect(res.status).toBe(200)
    expect(res.body.proceed).toBe(true)
  })

  it('returns pending list', async () => {
    const app = makeApp()
    const res = await request(app).get('/api/hooks/pending')
    expect(res.status).toBe(200)
    expect(Array.isArray(res.body.edits)).toBe(true)
  })
})
