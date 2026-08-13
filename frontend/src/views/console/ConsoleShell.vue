<script setup lang="ts">
import { BarChart3, Boxes, Building2, Gauge, KeyRound, Puzzle, Settings, ShieldCheck } from '@lucide/vue'
import ProductShell from '@/components/ProductShell.vue'

const navGroups = [
  {
    label: 'nav.enterpriseManagement',
    items: [
      { to: '/console/workbench', label: 'console.workbench', icon: Gauge },
      { to: '/console/applications', label: 'console.applications', icon: KeyRound },
      { to: '/console/model-services', label: 'console.modelServices', icon: Boxes }
    ]
  },
  {
    label: 'nav.policyManagement',
    items: [
      { to: '/console/policies/access', label: 'policy.access.title', icon: ShieldCheck },
      { to: '/console/policies/routing', label: 'policy.routing.title', icon: ShieldCheck }
    ]
  },
  {
    label: 'nav.systemManagement',
    items: [
      { to: '/console/usage', label: 'console.usageCost', icon: BarChart3 },
      { to: '/console/organization', label: 'console.organization', icon: Building2 },
      { to: '/console/system/plugins', label: 'admin.plugins', icon: Puzzle },
      { to: '/console/system', label: 'console.system', icon: Settings }
    ]
  }
]
</script>

<template>
  <ProductShell home-to="/console/workbench" nav-label="nav.console" entry="console" :nav-groups="navGroups" />
</template>
