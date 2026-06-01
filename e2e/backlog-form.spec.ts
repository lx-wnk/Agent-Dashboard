import process from 'node:process'
import { expect, test } from '@playwright/test'

// ---------------------------------------------------------------------------
// Backlog / "New Task" create form — single-screen flow.
//
// The old two-step wizard (project-radio-* / project-step-next / -skip) was
// removed; BacklogForm.vue is now a single screen rendered inside the
// "New Task" modal. The "+ New Task" CTA in the topbar only renders while the
// pipeline view is active, so each test pre-sets the persisted view via
// localStorage (addInitScript runs before any app script reads it).
//
// These tests hit the real Go backend (createTask → POST /api/tasks). The dev
// server runs with `cfg.Auth == "none"`, so unauthenticated requests are
// accepted. The server's CSRF guard rejects unsafe-method API calls without an
// Origin header, so APIRequestContext calls set it explicitly (see
// spawn-with-project.spec.ts for the same pattern).
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

test('create a plain task with a project auto-fills cwd and lands on the board', async ({ page, request, baseURL }) => {
  const slug = `e2e-create-${Date.now()}`
  const csrfHeaders = { Origin: baseURL ?? 'http://localhost:13120' }

  // 1. Pre-seed a project so "the first real project" is deterministic.
  const projectRes = await request.post('/api/projects', {
    headers: csrfHeaders,
    data: { name: `E2E ${slug}`, slug },
  })
  // toBeOK() surfaces the server's error body on failure.
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

    // 4. Select the pre-seeded project by its id. The watch() handler that
    //    fetches folders and fills cwd is async, so the next assertion relies
    //    on Playwright auto-retry.
    await page.getByTestId('backlog-project-select').selectOption(project.id)

    // 5. cwd must auto-fill from the project's default folder.
    await expect(page.getByTestId('details-cwd')).not.toHaveValue('')

    // 6. Fill a unique title (slug auto-derives) and submit.
    const title = `E2E create task ${slug}`
    await page.getByTestId('details-title').fill(title)
    await page.getByTestId('details-submit').click()

    // 7. After "Create" the modal closes and the task is selected — its title
    //    is visible (on the board card and/or the opened task detail).
    await expect(page.getByText(title)).toBeVisible()
  }
  finally {
    await request.delete(`/api/projects/${project.id}`, { headers: csrfHeaders })
  }
})

test('create & refine with no project opens the refinement chat', async ({ page }) => {
  await selectPipelineView(page)
  await page.goto('/')

  // Open the create modal.
  await page.getByRole('button', { name: '+ New Task' }).click()
  await expect(page.getByRole('heading', { name: 'New Task' })).toBeVisible()

  // "No project" is the default selection (value ""), so cwd is not
  // auto-filled — fill it manually along with the title.
  await page.getByTestId('backlog-project-select').selectOption('')
  await page.getByTestId('details-title').fill(`E2E refine task ${Date.now()}`)
  await page.getByTestId('details-cwd').fill('/tmp')

  // "Create & Refine" creates the task then opens the refinement chat modal.
  await page.getByTestId('details-submit-refine').click()

  // Assert the refinement chat empty-state copy from RefinementChat.vue.
  await expect(page.getByText('What would you like to build?')).toBeVisible()
})
