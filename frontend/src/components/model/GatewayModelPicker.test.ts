import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import i18n, { setLocale } from '@/i18n'
import GatewayModelPicker from './GatewayModelPicker.vue'

describe('GatewayModelPicker', () => {
  it('lists active gateway models and preserves historical selections', async () => {
    setLocale('en-US')
    const wrapper = mount(GatewayModelPicker, {
      props: {
        models: [
          { id: 'active-1', model_id: 'gateway-current', name: 'Current', status: 'active' },
          { id: 'active-2', model_id: 'gateway-fast', name: 'Fast', status: 'active' },
          { id: 'disabled', model_id: 'gateway-retired', name: 'Retired', status: 'disabled' }
        ] as never,
        modelValue: ['gateway-current', 'gateway-retired', 'legacy-model']
      },
      global: { plugins: [i18n] }
    })

    expect(wrapper.findAll('[data-model-state="active"]').map((item) => item.text())).toEqual([
      'gateway-current',
      'gateway-fast'
    ])
    expect(wrapper.findAll('[data-model-state="historical"]').map((item) => item.text())).toEqual([
      'gateway-retired',
      'legacy-model'
    ])

    await wrapper.findAll('[data-model-state="active"]')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([
      ['gateway-current', 'gateway-retired', 'legacy-model', 'gateway-fast']
    ])

    await wrapper.findAll('[data-model-state="historical"]')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([
      ['gateway-current', 'gateway-retired']
    ])
  })

  it('uses the caller-provided accessible group name', () => {
    setLocale('en-US')
    const wrapper = mount(GatewayModelPicker, {
      props: { models: [], modelValue: [], ariaLabel: 'Model denylist' },
      global: { plugins: [i18n] }
    })

    expect(wrapper.get('[role="group"]').attributes('aria-label')).toBe('Model denylist')
  })
})
