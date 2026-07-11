<script setup lang="ts">
import { Activity, Gauge, KeyRound, RadioTower, Route, Settings } from '@lucide/vue'
import SurfaceShell from '@/components/SurfaceShell.vue'

const navGroups = [
  {
    label: 'nav.operations',
    items: [
      { to: '/operator/overview', label: 'operator.overview', icon: Gauge },
      { to: '/operator/routing-groups', label: 'operator.groupList', icon: Route },
      { to: '/operator/resources', label: 'operator.resourceList', icon: RadioTower },
      { to: '/operator/keys', label: 'operator.keyList', icon: KeyRound },
      { to: '/operator/usage', label: 'operator.traffic', icon: Activity }
    ]
  },
  {
    label: 'nav.platform',
    items: [{ to: '/operator/settings', label: 'admin.settings', icon: Settings }]
  }
]
</script>

<template>
  <SurfaceShell home-to="/operator/overview" nav-label="nav.operator" surface="relay_operator" :nav-groups="navGroups" />
</template>
