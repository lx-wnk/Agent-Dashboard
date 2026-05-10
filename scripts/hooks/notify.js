#!/usr/bin/env node
// Claude Code lifecycle hook — fire-and-forget POST to the dashboard.
// Install by adding to ~/.claude/settings.json hooks (see docs/hooks-setup.md).
//
// Env vars (all optional):
//   DASHBOARD_HOOKS_URL    — target URL, default http://127.0.0.1:13120/api/hooks/event
//   DASHBOARD_HOOKS_SECRET — shared secret; must match DASHBOARD_HOOKS_SECRET on server
//   CLAUDE_HOOK_TYPE       — set automatically by Claude Code for each hook event

const { Buffer } = require('node:buffer')
const process = require('node:process')

const DASHBOARD_HOOKS_URL
  = process.env.DASHBOARD_HOOKS_URL
    || 'http://127.0.0.1:13120/api/hooks/event'
const DASHBOARD_HOOKS_SECRET = process.env.DASHBOARD_HOOKS_SECRET || ''

const chunks = []
process.stdin.on('data', c => chunks.push(c))
process.stdin.on('end', async () => {
  let body = {}
  try {
    body = JSON.parse(Buffer.concat(chunks).toString('utf-8'))
  }
  catch {
    // stdin may be empty for some hook types — not an error
  }

  const headers = { 'Content-Type': 'application/json' }
  if (DASHBOARD_HOOKS_SECRET) {
    headers.Authorization = `Bearer ${DASHBOARD_HOOKS_SECRET}`
  }

  try {
    await Promise.race([
      fetch(DASHBOARD_HOOKS_URL, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          hookType: process.env.CLAUDE_HOOK_TYPE || 'unknown',
          ...body,
        }),
      }),
      new Promise((_, rej) => setTimeout(() => rej(new Error('timeout')), 500)),
    ])
  }
  catch {
    // Hooks must never block the Claude Code session — swallow all errors
  }

  process.exit(0)
})
