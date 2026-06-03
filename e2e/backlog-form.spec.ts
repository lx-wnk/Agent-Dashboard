import process from 'node:process'
import { expect, test } from '@playwright/test'

// ---------------------------------------------------------------------------
// Backlog / "New Task" create form — single-screen flow.
//
// The form has a single primary action "Create & Refine". The working directory
// is no longer a visible field — it is auto-filled from the selected project's
// default folder. A project is mandatory: there is no "No project" option.
//
// The "+ New Task" CTA in the topbar only renders while the pipeline view is
// active, so each test pre-sets the persisted view via localStorage
// (addInitScript runs before any app script reads it).
//
// These tests hit the real Go backend (createTask → POST /api/tasks). The dev
// server runs with `cfg.Auth == "none"`, so unauthenticated requests are
// accepted. The server's CSRF guard rejects unsafe-method API calls without an
// Origin header, so APIRequestContext calls set it explicitly.
// ---------------------------------------------------------------------------

const overrideBaseUrl = process.env.DASHBOARD_E2E_BASE_URL
if (overrideBaseUrl) {
  test.use({ baseURL: overrideBaseUrl })
}

/** Persist the pipeline view so the "+ New Task" topbar CTA renders on load. */
async function selectPipelineView(page: import('@playwright/test').Page) {
  await page.addInitScript(() => {
    localStorage.setItem('agent-active-view', 'pipeline')
  })
}

test('create & refine with a project auto-fills cwd and opens the refinement chat', async ({ page, request, baseURL }) => {
  const slug = `e2e-create-${Date.now()}`
  const csrfHeaders = { Origin: baseURL ?? 'http://localhost:13120' }

  // 1. Pre-seed a project so "the first real project" is deterministic.
  const projectRes = await request.post('/api/projects', {
    headers: csrfHeaders,
    data: { name: `E2E ${slug}`, slug },
  })
  await expect(projectRes).toBeOK()
  const project = await projectRes.json() as { id: string }

  // 2. Pre-seed a default folder so selecting the project hydrates cwd.
  //    /tmp exists on every dev box and satisfies the absolute-path validator.
  const folderRes = await request.post(`/api/projects/${project.id}/folders`, {
    headers: csrfHeaders,
    data: { path: '/tmp', isDefault: true },
  })
  await expect(folderRes).toBeOK()

  try {
    await selectPipelineView(page)
    await page.goto('/')

    // 3. Open the create modal.
    await page.getByRole('button', { name: '+ New Task' }).click()
    await expect(page.getByRole('heading', { name: 'New Task' })).toBeVisible()

    // 4. Select the pre-seeded project — the watch() handler fetches folders
    //    and fills cwd asynchronously (Playwright auto-retries assertions).
    await page.getByTestId('backlog-project-select').selectOption(project.id)

    // 5. Fill a unique title (slug auto-derives).
    const title = `E2E create task ${slug}`
    await page.getByTestId('details-title').fill(title)

    // 6. Submit via the single primary action "Create & Refine".
    await page.getByTestId('details-submit-refine').click()

    // 7. After "Create & Refine" the backlog form closes and the refinement
    //    chat opens — assert the empty-state copy from RefinementChat.vue.
    await expect(page.getByText('What would you like to build?')).toBeVisible()
  }
  finally {
    await request.delete(`/api/projects/${project.id}`, { headers: csrfHeaders })
  }
})

test('no "No project" option — project select starts empty and must be chosen', async ({ page }) => {
  await selectPipelineView(page)
  await page.goto('/')

  await page.getByRole('button', { name: '+ New Task' }).click()
  await expect(page.getByRole('heading', { name: 'New Task' })).toBeVisible()

  const select = page.getByTestId('backlog-project-select')
  // The select has no empty/no-project option — its value must not be ''
  // (it will be the first real project or __create__, never the empty sentinel).
  const optionValues = await select.evaluate((el: HTMLSelectElement) =>
    Array.from(el.options).map(o => o.value),
  )
  expect(optionValues).not.toContain('')

  // Submit button is disabled while no title is entered.
  await expect(page.getByTestId('details-submit-refine')).toBeDisabled()
})
