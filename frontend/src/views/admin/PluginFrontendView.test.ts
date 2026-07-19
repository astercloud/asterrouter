import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiClientError } from '@/api/client'
import PluginFrontendView from './PluginFrontendView.vue'

const push = vi.fn()
const getPluginFrontendContribution = vi.fn()
const getPluginFrontendAsset = vi.fn()

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { pluginId: 'com.asterrouter.imagegen.workbench' }, path: '/admin/plugins/com.asterrouter.imagegen.workbench/workbench' }),
  useRouter: () => ({ push })
}))

vi.mock('@/api/plugins', () => ({
  getPluginFrontendContribution: (...args: unknown[]) => getPluginFrontendContribution(...args),
  getPluginFrontendAsset: (...args: unknown[]) => getPluginFrontendAsset(...args)
}))

describe('PluginFrontendView', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.clearAllMocks()
    document.querySelectorAll('[data-plugin-frontend]').forEach((node) => node.remove())
  })

  it('retries a newly installed frontend contribution before showing an error', async () => {
    vi.useFakeTimers()
    getPluginFrontendContribution
      .mockRejectedValueOnce(new ApiClientError('not found', 404, 1404))
      .mockResolvedValueOnce({
        schema_version: 'astercloud.plugin-frontend-contribution.v1',
        plugin_id: 'com.asterrouter.imagegen.workbench',
        surfaces: [{ surface: 'admin.plugins', slot: 'plugin-workbench', title: '图片生成工作台', asset: 'assets/index.js' }]
      })
    getPluginFrontendAsset.mockResolvedValue('')

    const wrapper = mount(PluginFrontendView)
    await vi.runAllTimersAsync()
    await flushPromises()

    expect(getPluginFrontendContribution).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('正在加载已安装插件工作台')
    wrapper.unmount()
  })

  it('ignores source-only asset keys while inlining packaged assets', async () => {
    getPluginFrontendContribution.mockResolvedValue({
      schema_version: 'astercloud.plugin-frontend-contribution.v1',
      plugin_id: 'com.asterrouter.imagegen.workbench',
      surfaces: [{ surface: 'admin.plugins', slot: 'plugin-workbench', title: '图片生成工作台', asset: 'assets/index.js' }]
    })
    getPluginFrontendAsset.mockImplementation((_id: string, path: string) => {
      if (path === 'assets/index.js') {
        return Promise.resolve('window.__imagegenAssets={"/assets/materials/source.webp":"/assets/output.webp"}')
      }
      if (path === 'assets/materials/source.webp') {
        return Promise.reject(new ApiClientError('not found', 404, 1404))
      }
      if (path === 'assets/output.webp') {
        return Promise.resolve(new Uint8Array([1, 2, 3]).buffer)
      }
      return Promise.reject(new Error(`unexpected asset path ${path}`))
    })

    const wrapper = mount(PluginFrontendView)
    await flushPromises()

    expect(getPluginFrontendAsset).toHaveBeenCalledWith('com.asterrouter.imagegen.workbench', 'assets/materials/source.webp', 'arraybuffer')
    expect(getPluginFrontendAsset).toHaveBeenCalledWith('com.asterrouter.imagegen.workbench', 'assets/output.webp', 'arraybuffer')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
