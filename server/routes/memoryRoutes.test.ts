import { describe, expect, it } from 'bun:test'
import express from 'express'
import request from 'supertest'
import { createMemoryRouter } from './memoryRoutes.js'

function makeApp() {
  const app = express()
  app.use(express.json())
  app.use('/api', createMemoryRouter())
  return app
}

describe('memoryRoutes path validation', () => {
  it('rejects path traversal in GET', async () => {
    const app = makeApp()
    const res = await request(app).get('/api/memory/..%2F..%2Fetc%2Fpasswd')
    expect(res.status).toBe(400)
    expect(res.body.error).toMatch(/traversal/i)
  })

  it('rejects path traversal in PUT', async () => {
    const app = makeApp()
    const res = await request(app)
      .put('/api/memory/..%2F..%2Fetc%2Fpasswd')
      .send({ content: 'x' })
    expect(res.status).toBe(400)
  })
})
