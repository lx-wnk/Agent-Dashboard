import process from 'node:process'
import { expect, test } from '@playwright/test'

// Allow the dev environment to override the dashboard URL — e.g. point at the
// Vite dev server (`http://localhost:5173`) when the Go backend on 13120 has
// no built frontend to serve. Defaults to the playwright.config.ts baseURL.
const overrideBaseUrl = process.env.DASHBOARD_E2E_BASE_URL
if (overrideBaseUrl) {
  test.use({ baseURL: overrideBaseUrl })
}

// ---------------------------------------------------------------------------
// Spawn dialog: project picker hydrates cwd from default folder
//
// Pre-seeds a project + a default folder via the REST API, then drives the
// SpawnDialog through the project picker and asserts that the working
// directory input is populated from the project's default folder. The dev
// server runs with `cfg.Auth == "none"` so unauthenticated requests are
// accepted. See dashboard.spec.ts for the broader auth/setup pattern.
// ---------------------------------------------------------------------------

test('spawn dialog shows project picker and hydrates cwd from default folder', async ({ page, request, baseURL }) => {
  const slug = `e2e-${Date.now()}`

  // The server's CSRF guard rejects unsafe-method requests without an Origin
  // header (`missing Origin header`, 403). Browser requests get this header
  // for free; APIRequestContext does not, so we set it explicitly for every
  // mutating call.
  const csrfHeaders = { Origin: baseURL ?? 'http://localhost:13120' }

  // 1. Pre-seed a project.
  const projectRes = await request.post('/api/projects', {
    headers: csrfHeaders,
    data: { name: `E2E ${slug}`, slug },
  })
  // toBeOK() includes the response body in failure messages — surfaces the
  // server's actual error text instead of a bare `expected true, got false`.
  await expect(projectRes).toBeOK()
  const project = await projectRes.json() as { id: string }

  // 2. Pre-seed a default folder. /tmp is safe (exists on every dev box) and
  //    must be an absolute path per the server's folder validator.
  const folderRes = await request.post(`/api/projects/${project.id}/folders`, {
    headers: csrfHeaders,
    data: { path: '/tmp', isDefault: true },
  })
  await expect(folderRes).toBeOK()

  try {
    // 3. Navigate.
    await page.goto('/')

    // 4. Open the spawn modal. The header button text is "+ New Agent".
    await page.getByRole('button', { name: '+ New Agent' }).click()

    // 5. Wait for the project select to render.
    await expect(page.locator('#spawn-project')).toBeVisible()

    // 6. Select the pre-seeded project. The composable's selectProject() is
    //    async (it may fetch folders) so we rely on Playwright auto-retry
    //    in the next assertion.
    await page.locator('#spawn-project').selectOption(project.id)

    // 7. Assert cwd hydrated. AppInput swallows the `id` prop (it uses
    //    useId() internally and the outer wrapper <div> receives the id via
    //    Vue attribute fallthrough), so we target the real <input> via the
    //    data-testid wrapper.
    const cwdInput = page.locator('[data-testid="spawn-cwd-wrap"] input')
    await expect(cwdInput).toHaveValue('/tmp')

    // 8. Cancel — we don't want to actually spawn a Claude process. Scope to
    //    the modal (AppModal renders role="dialog") so we don't accidentally
    //    match a Cancel button elsewhere on the page.
    await page.getByRole('dialog').getByRole('button', { name: /^cancel$/i }).click()
  }
  finally {
    // 9. Cleanup, even if an assertion above failed.
    await request.delete(`/api/projects/${project.id}`, { headers: csrfHeaders })
  }
})
