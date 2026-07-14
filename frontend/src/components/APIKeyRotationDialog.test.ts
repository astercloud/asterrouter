import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import APIKeyRotationDialog from './APIKeyRotationDialog.vue'

describe('APIKeyRotationDialog', () => {
  it('emits the selected grace period and resets when reopened', async () => {
    i18n.global.locale.value = 'en-US'
    const wrapper = mount(APIKeyRotationDialog, {
      props: { open: true, keyName: 'Production key' },
      global: { plugins: [i18n] },
      attachTo: document.body
	})

	await flushPromises()
    expect(wrapper.text()).toContain('Production key')
    expect(document.activeElement).toBe(wrapper.get('select').element)
    await wrapper.get('select').setValue('3600')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('confirm')).toEqual([[3600]])

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('0')
    wrapper.unmount()
  })

  it('emits cancel from the close button', async () => {
    const wrapper = mount(APIKeyRotationDialog, {
      props: { open: true, keyName: 'Service key' },
      global: { plugins: [i18n] }
    })
    await wrapper.get('.icon-button').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)

    await wrapper.get('form').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('cancel')).toHaveLength(2)
  })
})
