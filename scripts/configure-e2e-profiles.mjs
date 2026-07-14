const baseURL = process.env.ASTER_E2E_EXTERNAL_URL
const expectedProfile = process.env.ASTER_E2E_EXPECT_PROFILE

if (!baseURL || !expectedProfile) {
  process.stderr.write('ASTER_E2E_EXTERNAL_URL and ASTER_E2E_EXPECT_PROFILE are required.\n')
  process.exit(2)
}

async function envelope(response) {
  const payload = await response.json()
  if (!response.ok || payload.code !== 0) {
    throw new Error(`request failed status=${response.status} message=${payload.message || ''}`)
  }
  return payload.data
}

const settings = await envelope(await fetch(`${baseURL}/api/v1/settings/public`))
if (settings.default_profile !== expectedProfile || settings.enabled_profiles?.length !== 1 || settings.enabled_profiles[0] !== expectedProfile) {
  throw new Error(`expected a single ${expectedProfile} deployment profile, received ${JSON.stringify(settings)}`)
}

process.stdout.write(`Verified isolated ${expectedProfile} E2E deployment profile.\n`)
