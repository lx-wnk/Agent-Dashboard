import { expect, test } from '@playwright/test'

test('create task via two-step wizard with project selection', async ({ page }) => {
  await page.goto('/')

  // Seed: assume the dev fixture provides at least one project.
  await page.getByRole('button', { name: /new task|backlog/i }).click()

  // Step 1: pick first project.
  await page.locator('[data-testid^="project-radio-"]').first().click()
  await page.getByTestId('project-step-next').click()

  // Step 2: fill and submit.
  await page.getByTestId('details-title').fill('E2E wizard task')
  await page.getByTestId('details-slug').fill('e2e-wizard-task')
  // cwd should be prefilled — assert non-empty.
  const cwd = page.getByTestId('details-cwd')
  await expect(cwd).not.toHaveValue('')
  await page.getByTestId('details-submit').click()

  await expect(page.getByText('E2E wizard task')).toBeVisible()
})

test('create task via wizard with skip path', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: /new task|backlog/i }).click()
  await page.getByTestId('project-step-skip').click()
  await page.getByTestId('project-step-next').click()
  await page.getByTestId('details-title').fill('Skip path task')
  await page.getByTestId('details-slug').fill('skip-path-task')
  await page.getByTestId('details-cwd').fill('/tmp/skip-path-task')
  await page.getByTestId('details-submit').click()
  await expect(page.getByText('Skip path task')).toBeVisible()
})
