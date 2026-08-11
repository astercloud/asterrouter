<script setup lang="ts">
import { Activity, Code2, Gauge, KeyRound, UserRound } from '@lucide/vue'
import ProductShell from '@/components/ProductShell.vue'

const navGroups = [
  {
    label: 'nav.overview',
    items: [{ to: '/portal/overview', label: 'portal.overview', icon: Gauge }]
  },
  {
    label: 'nav.selfService',
    items: [
      { to: '/portal/applications', label: 'portal.applications', icon: KeyRound },
      { to: '/portal/access', label: 'portal.access', icon: Code2 }
    ]
  },
  {
    label: 'nav.activity',
    items: [
      { to: '/portal/usage', label: 'portal.usage', icon: Activity },
      { to: '/portal/account', label: 'account.title', icon: UserRound }
    ]
  }
]
</script>

<template>
  <ProductShell home-to="/portal/overview" nav-label="nav.portal" entry="portal" :nav-groups="navGroups" />
</template>
