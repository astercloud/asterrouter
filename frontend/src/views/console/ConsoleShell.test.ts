import { shallowMount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ProductShell from '@/components/ProductShell.vue'
import ConsoleShell from './ConsoleShell.vue'

interface NavigationGroup {
  label: string
  items: Array<{ to: string; label: string }>
}

describe('ConsoleShell', () => {
  it('keeps every canonical management page discoverable from the sidebar', () => {
    const wrapper = shallowMount(ConsoleShell)
    const groups = wrapper.getComponent(ProductShell).props('navGroups') as NavigationGroup[]

    expect(groups.map((group) => group.label)).toEqual([
      'nav.enterpriseManagement',
      'nav.inference',
      'nav.policyManagement',
      'nav.analytics',
      'nav.organization',
      'nav.systemManagement'
    ])
    expect(groups.flatMap((group) => group.items.map((item) => item.to))).toEqual([
      '/console/workbench',
      '/console/applications',
      '/console/applications/credentials',
      '/console/model-services/catalog',
      '/console/model-services',
      '/console/model-services/providers',
      '/console/model-services/accounts',
      '/console/model-services/routes',
      '/console/model-services/route-groups',
      '/console/model-services/simulator',
      '/console/model-services/pricing',
      '/console/model-services/effective-pricing',
      '/console/policies/access',
      '/console/policies/routing',
      '/console/usage',
      '/console/usage/supply',
      '/console/usage/cost-allocation',
      '/console/usage/traces',
      '/console/usage/alerts',
      '/console/usage/artifacts',
      '/console/usage/jobs',
      '/console/usage/exports',
      '/console/organization',
      '/console/organization/departments',
      '/console/organization/groups',
      '/console/system/plugins',
      '/console/system/audit',
      '/console/system'
    ])
  })
})
