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

async function verifyPortalApplicationsSpacing(page: Page): Promise<void> {
  const sections = ['access', 'current-key', 'key-list', 'security']
    .map((section) => page.locator(`[data-portal-section="${section}"]`))
  for (const section of sections) await expect(section).toBeVisible()

  const boxes = await Promise.all(sections.map((section) => section.boundingBox()))
  for (let index = 1; index < boxes.length; index += 1) {
    const previous = boxes[index - 1]
    const current = boxes[index]
    if (!previous || !current) throw new Error('Portal application section bounds are unavailable.')
    expect(current.y - previous.y - previous.height).toBeGreaterThanOrEqual(16)
  }
}

async function verifyEnterpriseShell(page: Page): Promise<void> {
  const viewport = page.viewportSize()
  if (!viewport) throw new Error('Viewport dimensions are unavailable.')

  const header = page.locator('[data-global-header]')
  const tabbar = page.locator('[data-shell-tabs]')
  await expect(header).toBeVisible()
  await expect(tabbar).toBeVisible()
  const headerBox = await header.boundingBox()
  const tabbarBox = await tabbar.boundingBox()
  if (!headerBox || !tabbarBox) throw new Error('Enterprise shell bounds are unavailable.')
  expect(Math.abs(headerBox.height - 48)).toBeLessThanOrEqual(1)
  expect(Math.abs(tabbarBox.height - 36)).toBeLessThanOrEqual(1)
  expect(Math.abs(headerBox.width - viewport.width)).toBeLessThanOrEqual(1)

  const sidebar = page.locator('[data-shell-sidebar]')
  const sidebarBox = await sidebar.boundingBox()
  if (!sidebarBox) throw new Error('Enterprise sidebar bounds are unavailable.')
  if (viewport.width > 920) {
    expect(Math.abs(sidebarBox.width - 200)).toBeLessThanOrEqual(1)
    expect(Math.abs(sidebarBox.y - 48)).toBeLessThanOrEqual(1)
  } else {
    await expect.poll(async () => {
      const box = await sidebar.boundingBox()
      return box ? box.x + box.width : Number.POSITIVE_INFINITY
    }, { timeout: 2_000 }).toBeLessThanOrEqual(1)
  }
}

async function verifyPluginCenterDensity(page: Page): Promise<void> {
  const pageRoot = page.locator('.plugin-center-page')
  const header = pageRoot.locator('.page-header')
  const tabs = pageRoot.locator('.plugin-center-tabs')
  const dashboard = pageRoot.locator('[data-section="workbench"]')
  await expect(pageRoot).toBeVisible()
  await expect(tabs).toBeVisible()
  await expect(dashboard).toBeVisible()

  const geometry = await page.evaluate(() => {
    const root = document.querySelector('.plugin-center-page')
    const header = root?.querySelector('.page-header')
    const tabs = root?.querySelector('.plugin-center-tabs')
    const dashboard = root?.querySelector('[data-section="workbench"]')
    if (!root || !header || !tabs || !dashboard) return null
    const box = (element: Element) => {
      const rect = element.getBoundingClientRect()
      return { top: rect.top, bottom: rect.bottom, height: rect.height }
    }
    return {
      root: box(root),
      header: box(header),
      tabs: box(tabs),
      dashboard: box(dashboard),
      alignContent: getComputedStyle(root).alignContent
    }
  })
  expect(geometry).not.toBeNull()
  expect(geometry?.alignContent).toBe('start')
  expect(geometry!.tabs.top - geometry!.header.bottom).toBeGreaterThanOrEqual(8)
  expect(geometry!.tabs.top - geometry!.header.bottom).toBeLessThanOrEqual(28)
  expect(geometry!.dashboard.top - geometry!.tabs.bottom).toBeGreaterThanOrEqual(8)
  expect(geometry!.dashboard.top - geometry!.tabs.bottom).toBeLessThanOrEqual(28)
}

async function verifyEffectivePricingDensity(page: Page): Promise<void> {
  const root = page.locator('.effective-pricing-page')
  const header = root.locator('.page-header')
  const tabs = root.locator('.effective-tabs')
  const filters = root.locator('.effective-filters')
  const metrics = root.locator('.metric-grid').first()
  const panel = root.locator('.effective-panel').first()
  await expect(root).toBeVisible()
  await expect(tabs).toBeVisible()
  await expect(filters).toBeVisible()
  await expect(metrics).toBeVisible()
  await expect(panel).toBeVisible()

  const geometry = await page.evaluate(() => {
    const root = document.querySelector('.effective-pricing-page')
    const header = root?.querySelector('.page-header')
    const tabs = root?.querySelector('.effective-tabs')
    const filters = root?.querySelector('.effective-filters')
    const metrics = root?.querySelector('.metric-grid')
    const panel = root?.querySelector('.effective-panel')
    if (!root || !header || !tabs || !filters || !metrics || !panel) return null
    const box = (element: Element) => {
      const rect = element.getBoundingClientRect()
      return { top: rect.top, bottom: rect.bottom, height: rect.height }
    }
    return {
      root: box(root),
      header: box(header),
      tabs: box(tabs),
      filters: box(filters),
      metrics: box(metrics),
      panel: box(panel),
      alignContent: getComputedStyle(root).alignContent
    }
  })
  expect(geometry).not.toBeNull()
  expect(geometry?.alignContent).toBe('start')
  expect(geometry!.tabs.top - geometry!.header.bottom).toBeGreaterThanOrEqual(8)
  expect(geometry!.tabs.top - geometry!.header.bottom).toBeLessThanOrEqual(28)
  expect(geometry!.filters.top - geometry!.tabs.bottom).toBeGreaterThanOrEqual(8)
  expect(geometry!.filters.top - geometry!.tabs.bottom).toBeLessThanOrEqual(28)
  expect(geometry!.metrics.top - geometry!.filters.bottom).toBeGreaterThanOrEqual(8)
  expect(geometry!.metrics.top - geometry!.filters.bottom).toBeLessThanOrEqual(28)
  expect(geometry!.panel.top - geometry!.metrics.bottom).toBeGreaterThanOrEqual(8)
  expect(geometry!.panel.top - geometry!.metrics.bottom).toBeLessThanOrEqual(28)
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
  await verifyEnterpriseShell(page)
  await verifyPluginCenterDensity(page)
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
      if (surface.route === '/console/model-services/effective-pricing') await verifyEffectivePricingDensity(page)
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
    await test.step(surface.route, async () => {
      await verifySurface(page, surface)
      if (surface.route === '/portal/applications') await verifyPortalApplicationsSpacing(page)
    })
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
