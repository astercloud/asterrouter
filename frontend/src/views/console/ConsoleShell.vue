<script setup lang="ts">
import { BarChart3, Boxes, FlaskConical, Gauge, KeyRound, Plug, RadioTower, Route, Server, Settings } from '@lucide/vue'
import SurfaceShell from '@/components/SurfaceShell.vue'

const navGroups = [
  {
    label: 'nav.overview',
    items: [{ to: '/console/overview', label: 'console.overview', icon: Gauge }]
  },
  {
    label: 'nav.inference',
    items: [
      { to: '/console/providers', label: 'admin.providers', icon: Server },
      { to: '/console/models', label: 'admin.gatewayModels', icon: Boxes },
      { to: '/console/model-routes', label: 'admin.modelRoutes', icon: Route },
      { to: '/console/gateway-simulator', label: 'admin.gatewaySimulator', icon: FlaskConical },
      { to: '/console/routing-groups', label: 'admin.routingGroups', icon: Route },
      { to: '/console/resources', label: 'admin.providerAccounts', icon: RadioTower }
    ]
  },
  {
    label: 'nav.access',
    items: [{ to: '/console/keys', label: 'console.keys', icon: KeyRound }]
  },
  {
    label: 'nav.analytics',
    items: [{ to: '/console/usage', label: 'console.usage', icon: BarChart3 }]
  },
  {
    label: 'nav.platform',
    items: [
      { to: '/console/plugins', label: 'admin.plugins', icon: Plug },
      { to: '/console/settings', label: 'admin.settings', icon: Settings }
    ]
  }
]
</script>

<template>
  <SurfaceShell home-to="/console/overview" nav-label="nav.console" surface="personal" :nav-groups="navGroups" />
</template>
