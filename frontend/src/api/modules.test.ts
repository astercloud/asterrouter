import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as account from './account'
import * as auth from './auth'
import * as plugins from './plugins'
import * as settings from './settings'
import * as system from './system'

const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))

describe('enterprise API module contracts', () => {
  beforeEach(() => {
    for (const method of Object.values(client)) method.mockReset()
    client.get.mockResolvedValue({ data: {} })
    client.post.mockResolvedValue({ data: {} })
    client.put.mockResolvedValue({ data: {} })
    client.delete.mockResolvedValue({ data: {} })
  })

  it('uses the authentication endpoint contracts', async () => {
    const result = { access_token: 'token', token_type: 'Bearer', expires_at: '2099-01-01T00:00:00Z', user: { username: 'user', role: 'developer' } }
    client.post.mockResolvedValueOnce({ data: result })
    expect(await auth.login('user', 'secret', true, 'turnstile')).toEqual(result)
    expect(client.post).toHaveBeenLastCalledWith('/auth/login', {
      username: 'user', password: 'secret', agreement_accepted: true, turnstile_token: 'turnstile', session_mode: 'cookie'
    })
    await auth.logout()
    expect(client.post).toHaveBeenLastCalledWith('/auth/logout')
  })

  it('uses enterprise setup and console settings endpoints', async () => {
    await settings.getPublicSettings()
    expect(client.get).toHaveBeenLastCalledWith('/settings/public')
    await settings.getAdminSettings()
    expect(client.get).toHaveBeenLastCalledWith('/console/settings')
    const payload = { site_name: 'Test' } as never
    await settings.updateAdminSettings(payload)
    expect(client.put).toHaveBeenLastCalledWith('/console/settings', payload)
    await settings.completeEnterpriseSetup('Nebula Technologies')
    expect(client.post).toHaveBeenLastCalledWith('/setup', { organization_name: 'Nebula Technologies' })
  })

  it('normalizes nullable enterprise collections', async () => {
    for (const load of [settings.getDefaultEmailTemplates, settings.getLocales]) {
      client.get.mockResolvedValueOnce({ data: null })
      expect(await load()).toEqual([])
    }
    for (const load of [
      () => plugins.getArtifactSinkDestinations('plugin-1'),
      () => plugins.getPluginAPITokens(),
      () => plugins.getOfficialFeedStatuses(),
      () => plugins.getOfficialFeedSyncRuns(),
      () => plugins.getPluginDeliveries('plugin-1'),
      () => plugins.getPluginPackages('plugin-1'),
      system.listSystemBackups,
      system.listS3Backups
    ]) {
      client.get.mockResolvedValueOnce({ data: null })
      expect(await load()).toEqual([])
    }
    client.get.mockResolvedValueOnce({ data: { auth_identities: null, login_methods: null } })
    expect(await account.getAccountProfile()).toMatchObject({ auth_identities: [], login_methods: [] })
  })

  it('uses account security endpoint contracts', async () => {
    await account.updateAccountProfile('Updated', 'data:image/png;base64,AA==')
    expect(client.put).toHaveBeenLastCalledWith('/account/profile', { display_name: 'Updated', avatar_data_url: 'data:image/png;base64,AA==' })
    await account.changeAccountPassword('current', 'new-password-123')
    expect(client.put).toHaveBeenLastCalledWith('/account/password', { current_password: 'current', new_password: 'new-password-123' })
    await account.revokeOtherAccountSessions()
    expect(client.post).toHaveBeenLastCalledWith('/account/sessions/revoke-others')
  })
})
