<script setup lang="ts">
import { BarChart3, Gauge, KeyRound, Settings } from '@lucide/vue'
import SurfaceShell from '@/components/SurfaceShell.vue'

const navGroups = [
  {
    label: 'nav.workspace',
    items: [
      { to: '/console/overview', label: 'console.overview', icon: Gauge },
      { to: '/console/keys', label: 'console.keys', icon: KeyRound },
      { to: '/console/usage', label: 'console.usage', icon: BarChart3 }
    ]
  },
  {
    label: 'nav.platform',
    items: [{ to: '/console/settings', label: 'admin.settings', icon: Settings }]
  }
]
</script>

<template>
  <SurfaceShell home-to="/console/overview" nav-label="nav.console" surface="personal" :nav-groups="navGroups" />
</template>
