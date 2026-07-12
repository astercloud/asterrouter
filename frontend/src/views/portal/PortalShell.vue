<script setup lang="ts">
import { Activity, BellRing, Code2, Gauge, KeyRound, Route } from '@lucide/vue'
import SurfaceShell from '@/components/SurfaceShell.vue'

const navGroups = [
  {
    label: 'nav.overview',
    items: [{ to: '/portal/overview', label: 'portal.overview', icon: Gauge }]
  },
  {
    label: 'nav.selfService',
    items: [
      { to: '/portal/integration', label: 'portal.integrationGuide', icon: Code2 },
      { to: '/portal/keys', label: 'portal.myKeys', icon: KeyRound }
    ]
  },
  {
    label: 'nav.activity',
    items: [
      { to: '/portal/usage', label: 'portal.usage', icon: Activity },
      { to: '/portal/alerts', label: 'portal.alerts', icon: BellRing },
      { to: '/portal/traces', label: 'portal.recentTraces', icon: Route }
    ]
  }
]
</script>

<template>
  <SurfaceShell home-to="/portal/overview" nav-label="nav.portal" surface="portal" :nav-groups="navGroups" />
</template>
