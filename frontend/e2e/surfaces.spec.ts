import { readFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'
import {
  captureBrowserErrors,
  envelope,
  expectNoHorizontalOverflow,
  loginDemo,
  loginTestPrincipal,
  registerUsers
} from './fixtures'

type Surface = { route: string; path?: string; target?: 'heading' | 'alert' }
type RegistryRoute = { path: string; surface: string }

const registry = JSON.parse(readFileSync(new URL('../../docs/test/v1/scenario-registry.json', import.meta.url), 'utf8')) as { routes: RegistryRoute[] }
const surfaceOverrides: Record<string, Omit<Surface, 'route'>> = {
  '/': { target: 'heading' },
  '/login': { target: 'heading' },
  '/register': { target: 'heading' },
  '/forgot-password': { target: 'heading' },
  '/resend-verification': { target: 'heading' },
  '/reset-password': { path: '/reset-password?token=surface-contract', target: 'heading' },
  '/verify-email': { target: 'alert' },
  '/legal/:slug': { path: '/legal/surface-contract-missing', target: 'alert' },
  '/console/system/plugins/:pluginId/workbench': { path: '/console/system/plugins/surface-contract-missing/workbench' }
}

function surfacesFor(scenarioID: string): Surface[] {
  return registry.routes
    .filter((route) => route.surface === scenarioID)
    .map((route) => ({ route: route.path, ...surfaceOverrides[route.path] }))
}

const publicSurfaces = surfacesFor('@e2e-surface-public-001')
const consoleSurfaces = surfacesFor('@e2e-surface-console-001')
const portalSurfaces = surfacesFor('@e2e-surface-portal-001')

function escapedPath(path: string): RegExp {
  return new RegExp(`${path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`)
}

async function verifySurface(page: Page, surface: Surface): Promise<void> {
  const path = surface.path || surface.route
  await page.goto(path)
  expect(new URL(page.url()).pathname).toBe(new URL(path, page.url()).pathname)
  if (surface.target === 'alert') await expect(page.getByRole('alert')).toBeVisible()
  else if (surface.target === 'heading') await expect(page.getByRole('heading').first()).toBeVisible()
  else await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await expectNoHorizontalOverflow(page)
}

async function loginThroughPage(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('Username').fill(email)
  await page.locator('#password').fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(/\/portal\/overview$/)
}

test('@e2e-surface-public-001 public routes remain reachable and correctly projected', async ({ page }) => {
  const errors = captureBrowserErrors(page)
  for (const surface of publicSurfaces) {
    await test.step(surface.route, () => verifySurface(page, surface))
  }
  expect(errors.filter((error) => !error.includes('404 (Not Found)'))).toEqual([])
})

test('@e2e-surface-console-001 console routes remain reachable across supported viewports', async ({ page }) => {
  test.setTimeout(90_000)
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const menuButton = page.getByRole('button', { name: 'Open navigation' })
  if (await menuButton.isVisible()) await menuButton.click()
  const pluginCenterLink = page.getByRole('link', { name: 'Plugin Center', exact: true })
  await expect(pluginCenterLink).toBeVisible()
  await pluginCenterLink.click()
  await expect(page).toHaveURL(escapedPath('/console/system/plugins'))
  await expect(page.getByRole('heading', { level: 1, name: 'Plugin Center' })).toBeVisible()
  for (const surface of consoleSurfaces) {
    await test.step(surface.route, async () => {
      const previousErrorCount = errors.length
      const missingWorkbenchResponse = surface.route.includes(':pluginId')
        ? page.waitForResponse((response) => {
            const url = new URL(response.url())
            return response.request().method() === 'GET' &&
              url.pathname === '/api/v1/console/plugins/surface-contract-missing/frontend/workbench'
          })
        : undefined
      await verifySurface(page, surface)
      if (surface.route.includes(':pluginId')) {
        expect((await missingWorkbenchResponse!).status()).toBe(404)
        await expect(page.getByRole('alert')).toContainText('not available')
        const expectedErrors = errors.splice(previousErrorCount)
        for (const error of expectedErrors) {
          expect(error).toBe('console: Failed to load resource: the server responded with a status of 404 (Not Found)')
        }
      }
    })
  }
  expect(errors).toEqual([])
})

test('@e2e-surface-portal-001 portal routes remain reachable across supported viewports', async ({ page }, testInfo) => {
  test.setTimeout(60_000)
  const errors = captureBrowserErrors(page)
  await loginDemo(page)
  const adminToken = await loginTestPrincipal(page)
  const password = 'synthetic-password-123'
  const [developer] = await registerUsers(page, adminToken, [{
    email: `surface-${testInfo.project.name}-${Date.now()}@example.test`,
    password,
    displayName: 'Portal Surface User'
  }])
  await page.context().clearCookies()
  await page.evaluate(() => localStorage.clear())
  await loginThroughPage(page, developer.email, password)

  for (const surface of portalSurfaces) {
    await test.step(surface.route, () => verifySurface(page, surface))
  }
  await page.goto('/console/workbench')
  await expect(page).toHaveURL(/\/portal\/overview$/)
  expect(errors).toEqual([])
})

test('@e2e-legal-001 legal documents are public and unknown slugs fail visibly', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium-desktop', 'The legal document lifecycle is viewport-independent; public surface coverage runs in every viewport.')

  await loginDemo(page)
  const token = await loginTestPrincipal(page)
  const headers = { Authorization: `Bearer ${token}` }
  const settings = await envelope<Record<string, unknown>>(await page.request.get('/api/v1/console/settings', { headers }))
  const slug = `browser-terms-${Date.now()}`
  const document = { id: slug, name: 'Browser Terms', slug, content: 'Synthetic enterprise terms.' }
  try {
    await envelope(await page.request.put('/api/v1/console/settings', {
      headers,
      data: { ...settings, legal_documents: [...(settings.legal_documents as unknown[] || []), document] }
    }))
    await page.context().clearCookies()
    await page.evaluate(() => localStorage.clear())
    await page.goto(`/legal/${slug}`)
    await expect(page).toHaveURL(escapedPath(`/legal/${slug}`))
    await expect(page.getByRole('heading', { level: 1, name: document.name })).toBeVisible()
    await expect(page.getByText(document.content)).toBeVisible()

    await page.goto('/legal/unknown-browser-document')
    await expect(page.getByRole('alert')).toContainText(/not available/i)
  } finally {
    await envelope(await page.request.put('/api/v1/console/settings', { headers, data: settings }))
  }
})
