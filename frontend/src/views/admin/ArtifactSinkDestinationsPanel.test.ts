import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import * as pluginAPI from '@/api/plugins'
import type { ArtifactSinkDestination } from '@/types'
import ArtifactSinkDestinationsPanel from './ArtifactSinkDestinationsPanel.vue'

vi.mock('@/api/plugins', () => ({
  deleteArtifactSinkDestination: vi.fn(),
  getArtifactSinkDestinations: vi.fn(),
  upsertArtifactSinkDestination: vi.fn()
}))

const destination: ArtifactSinkDestination = {
  id: 'customer-media',
  name: 'Customer media',
  provider: 'r2',
  endpoint: 'https://account.r2.cloudflarestorage.com',
  region: 'auto',
  bucket: 'customer-media',
  prefix: 'generated',
  reference_base_url: 'https://media.example/generated',
  allowed_profile_scope: 'platform',
  allowed_tenant_id: 'tenant-a',
  path_style: true,
  enabled: true,
  secret_hints: {
    access_key: 'ac...ey',
    secret_key: 'se...ey',
    session_token: 'to...en'
  }
}

function mountPanel() {
  return mount(ArtifactSinkDestinationsPanel, {
    props: { pluginId: 'com.asterrouter.artifact.s3-compatible-sink' },
    global: { plugins: [i18n] }
  })
}

describe('ArtifactSinkDestinationsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('en-US')
    vi.mocked(pluginAPI.getArtifactSinkDestinations).mockResolvedValue([])
    vi.mocked(pluginAPI.upsertArtifactSinkDestination).mockResolvedValue(destination)
    vi.mocked(pluginAPI.deleteArtifactSinkDestination).mockResolvedValue()
  })

  it('creates a destination and sends secrets only in the write request', async () => {
    vi.mocked(pluginAPI.getArtifactSinkDestinations)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([destination])
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('button.button.secondary').trigger('click')
    await wrapper.get('#artifact-sink-id').setValue('customer-media')
    await wrapper.get('#artifact-sink-name').setValue('Customer media')
    await wrapper.get('#artifact-sink-region').setValue('us-east-1')
    await wrapper.get('#artifact-sink-bucket').setValue('customer-media')
    await wrapper.get('#artifact-sink-access-key').setValue('plain-access')
    await wrapper.get('#artifact-sink-secret-key').setValue('plain-secret')
    await wrapper.get('form[role="dialog"]').trigger('submit')
    await flushPromises()

    expect(pluginAPI.upsertArtifactSinkDestination).toHaveBeenCalledWith(
      'com.asterrouter.artifact.s3-compatible-sink',
      'customer-media',
      expect.objectContaining({
        name: 'Customer media',
        provider: 's3',
        bucket: 'customer-media',
        secrets: { access_key: 'plain-access', secret_key: 'plain-secret' }
      })
    )
    expect(wrapper.find('form[role="dialog"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Customer media')
    expect(wrapper.text()).not.toContain('plain-access')
    expect(wrapper.text()).not.toContain('plain-secret')
  })

  it('keeps stored credentials and can explicitly clear a session token', async () => {
    vi.mocked(pluginAPI.getArtifactSinkDestinations)
      .mockResolvedValueOnce([destination])
      .mockResolvedValueOnce([{ ...destination, name: 'Updated media', secret_hints: { access_key: 'ac...ey', secret_key: 'se...ey' } }])
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('button[aria-label="Edit Customer media"]').trigger('click')
    expect((wrapper.get('#artifact-sink-access-key').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('#artifact-sink-access-key').attributes('placeholder')).toBe('ac...ey')
    expect(wrapper.get('#artifact-sink-secret-key').attributes('placeholder')).toBe('se...ey')
    await wrapper.get('#artifact-sink-name').setValue('Updated media')
    const clearToken = wrapper.findAll('input[type="checkbox"]').find((input) => input.element.parentElement?.textContent?.includes('Remove the stored session token'))
    expect(clearToken).toBeTruthy()
    await clearToken!.setValue(true)
    await wrapper.get('form[role="dialog"]').trigger('submit')
    await flushPromises()

    expect(pluginAPI.upsertArtifactSinkDestination).toHaveBeenCalledWith(
      'com.asterrouter.artifact.s3-compatible-sink',
      'customer-media',
      expect.objectContaining({
        name: 'Updated media',
        secrets: {},
        clear_session_token: true
      })
    )
  })

  it('requires confirmation before deleting a destination', async () => {
    vi.mocked(pluginAPI.getArtifactSinkDestinations).mockResolvedValue([destination])
    const confirm = vi.fn(() => true)
    vi.stubGlobal('confirm', confirm)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('button[aria-label="Delete Customer media"]').trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledWith('Delete delivery destination "Customer media"?')
    expect(pluginAPI.deleteArtifactSinkDestination).toHaveBeenCalledWith('com.asterrouter.artifact.s3-compatible-sink', 'customer-media')
    expect(wrapper.text()).not.toContain('Customer media')
    vi.unstubAllGlobals()
  })

  it('renders the destination workflow in Simplified Chinese', async () => {
    setLocale('zh-CN')
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain('交付目标')
    expect(wrapper.text()).toContain('添加目标')
  })
})
