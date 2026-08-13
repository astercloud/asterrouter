import { defineConfig, devices } from '@playwright/test'

const frontendPort = process.env.ASTER_E2E_FRONTEND_PORT || '15173'
const backendPort = process.env.ASTER_E2E_BACKEND_PORT || '18080'
const upstreamPort = process.env.ASTER_E2E_UPSTREAM_PORT || '19000'
const smtpPort = process.env.ASTER_E2E_SMTP_PORT || '29001'
const mailAPIPort = process.env.ASTER_E2E_MAIL_API_PORT || '29002'
const s3Port = process.env.ASTER_E2E_S3_PORT || '29003'
const s3APIPort = process.env.ASTER_E2E_S3_API_PORT || '29004'
const externalURL = process.env.ASTER_E2E_EXTERNAL_URL
const oidcProxyPort = process.env.ASTER_E2E_OIDC_PORT || '29005'
const officialPort = process.env.ASTER_E2E_OFFICIAL_PORT || '29006'
const storageDatabaseURL = process.env.ASTERROUTER_SERVER_STORAGE_DATABASE_URL
const storageDatabaseName = (() => {
  if (!storageDatabaseURL) return ''
  try {
    return decodeURIComponent(new URL(storageDatabaseURL).pathname.replace(/^\/+/, ''))
  } catch {
    return ''
  }
})()
const postgresAvailable = Boolean(storageDatabaseURL) || process.env.ASTER_E2E_POSTGRES_AVAILABLE === '1'
const isolatedOIDC = !externalURL && !storageDatabaseURL
const oidcEnabled = isolatedOIDC || process.env.ASTER_E2E_OIDC_ENABLED === '1'
const baseURL = externalURL || (oidcEnabled ? `https://127.0.0.1:${oidcProxyPort}` : `http://127.0.0.1:${frontendPort}`)
const artifactDir = process.env.ASTER_E2E_ARTIFACT_DIR
const chromiumChannel = process.env.ASTER_E2E_CHROMIUM_CHANNEL
const artifactPath = (relative: string) => artifactDir ? `${artifactDir}/${relative}` : `./${relative}`
const artifactStoreDir = artifactDir ? `${artifactDir}/runtime-artifacts` : `${process.cwd()}/test-results/runtime-artifacts`
const runtimeDataDir = artifactDir ? `${artifactDir}/runtime-data` : `${process.cwd()}/test-results/runtime-data`
const chromiumUse = chromiumChannel ? { channel: chromiumChannel } : {}
const videoMode = process.env.ASTER_E2E_VIDEO === 'off' ? 'off' as const : 'retain-on-failure' as const

process.env.ASTER_E2E_SMTP_PORT = smtpPort
process.env.ASTER_E2E_MAIL_API_URL ||= `http://127.0.0.1:${mailAPIPort}`
process.env.ASTER_E2E_S3_PORT = s3Port
process.env.ASTER_E2E_S3_API_URL ||= `http://127.0.0.1:${s3APIPort}`
process.env.ASTER_E2E_OIDC_AVAILABLE = oidcEnabled ? '1' : '0'
process.env.ASTER_E2E_POSTGRES_AVAILABLE = postgresAvailable ? '1' : '0'
process.env.ASTER_E2E_DATABASE_NAME ||= storageDatabaseName
process.env.ASTER_E2E_OFFICIAL_URL ||= `http://127.0.0.1:${officialPort}`

export default defineConfig({
  testDir: './e2e',
  outputDir: artifactPath('test-results'),
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  // Setup requires a dedicated empty runtime. It is executed by
  // test-setup-browser-journey.sh, not against the reusable demo server.
  grepInvert: process.env.ASTER_E2E_INCLUDE_SETUP === '1' ? undefined : /@setup/,
  retries: Number(process.env.ASTER_E2E_RETRIES || '0'),
  // Several journeys temporarily update global registration settings while
  // creating isolated synthetic users. Keep the default deterministic; an
  // explicit worker count is required for a deliberate parallelism exercise.
  workers: Number(process.env.ASTER_E2E_WORKERS || '1'),
  reporter: process.env.CI
    ? [['line'], ['html', { outputFolder: artifactPath('playwright-report'), open: 'never' }], ['junit', { outputFile: artifactPath('test-results/junit.xml') }]]
    : [['list'], ['html', { outputFolder: artifactPath('playwright-report'), open: 'never' }]],
  use: {
    baseURL,
    ignoreHTTPSErrors: oidcEnabled,
    actionTimeout: 10_000,
    navigationTimeout: 20_000,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: videoMode
  },
  expect: {
    timeout: 10_000
  },
  timeout: 30_000,
  projects: [
    {
      name: 'chromium-desktop',
      use: { ...devices['Desktop Chrome'], ...chromiumUse, viewport: { width: 1440, height: 900 } }
    },
    {
      name: 'chromium-compact',
      use: { ...devices['Desktop Chrome'], ...chromiumUse, viewport: { width: 1280, height: 800 } }
    },
    {
      name: 'chromium-mobile',
      use: { ...devices['Pixel 7'], ...chromiumUse, viewport: { width: 390, height: 844 } }
    },
    ...(process.env.ASTER_E2E_ALL_BROWSERS
      ? [
          { name: 'firefox-desktop', use: { ...devices['Desktop Firefox'], viewport: { width: 1440, height: 900 } } },
          { name: 'webkit-desktop', use: { ...devices['Desktop Safari'], viewport: { width: 1440, height: 900 } } }
        ]
      : [])
  ],
  webServer: externalURL
    ? undefined
    : {
        command: [
          'ASTER_DEV_KILL_OCCUPIED=0',
          `ASTER_DEV_BACKEND_PORT=${backendPort}`,
          `ASTER_DEV_FRONTEND_PORT=${frontendPort}`,
          `VITE_DEV_PROXY_TARGET=http://127.0.0.1:${backendPort}`,
          `ASTER_E2E_UPSTREAM_PORT=${upstreamPort}`,
          `ASTER_E2E_SMTP_PORT=${smtpPort}`,
          `ASTER_E2E_MAIL_API_PORT=${mailAPIPort}`,
          `ASTER_E2E_S3_PORT=${s3Port}`,
          `ASTER_E2E_S3_API_PORT=${s3APIPort}`,
          `ASTER_E2E_OIDC_PORT=${oidcProxyPort || '29005'}`,
          `ASTER_E2E_OFFICIAL_PORT=${officialPort}`,
          `ASTER_E2E_OIDC_ENABLED=${oidcEnabled ? '1' : '0'}`,
          'ASTER_E2E_OIDC_CLIENT_ID=asterrouter-e2e',
          'ASTER_E2E_OIDC_CLIENT_SECRET=asterrouter-e2e-secret',
          `ASTER_DEV_ISOLATED_MEMORY=${storageDatabaseURL ? '0' : '1'}`,
          'ASTERROUTER_SERVER_ARTIFACTS_DRIVER=local',
          `ASTERROUTER_SERVER_ARTIFACTS_LOCAL_ROOT=${JSON.stringify(artifactStoreDir)}`,
          `ASTERROUTER_SERVER_PLUGINS_CACHE_DIR=${JSON.stringify(`${runtimeDataDir}/plugin-cache`)}`,
          `ASTERROUTER_SERVER_PLUGINS_ACTIVE_DIR=${JSON.stringify(`${runtimeDataDir}/plugin-active`)}`,
          `ASTERROUTER_SERVER_PLUGINS_DATA_DIR=${JSON.stringify(`${runtimeDataDir}/plugin-data`)}`,
          `ASTERROUTER_SERVER_MAINTENANCE_BACKUP_DIR=${JSON.stringify(`${runtimeDataDir}/backups`)}`,
          `ASTERROUTER_SERVER_MAINTENANCE_DIAGNOSTIC_DIR=${JSON.stringify(`${runtimeDataDir}/diagnostics`)}`,
          'ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE=true',
          'ASTERROUTER_SERVER_SECURITY_SECRET_KEY=asterrouter-e2e-test-secret',
          'bash ../scripts/e2e.sh'
        ].join(' '),
        url: `http://127.0.0.1:${backendPort}/ready`,
        reuseExistingServer: false,
        timeout: 120_000,
        stdout: 'pipe',
        stderr: 'pipe'
      }
})
