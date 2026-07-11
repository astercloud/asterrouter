<script setup lang="ts">
import {
  Activity,
  BadgeDollarSign,
  BellRing,
  Building2,
  FileClock,
  FileOutput,
  Gauge,
  KeyRound,
  PieChart,
  Plug,
  Settings,
  Server,
  ShieldCheck,
  UsersRound,
  WalletCards
} from '@lucide/vue'
import SurfaceShell from '@/components/SurfaceShell.vue'

const navGroups = [
  {
    label: 'nav.governance',
    items: [
      { to: '/admin/dashboard', label: 'admin.overview', icon: Gauge },
      { to: '/admin/providers', label: 'admin.providers', icon: Server },
      { to: '/admin/model-pricings', label: 'admin.modelPricings', icon: BadgeDollarSign },
      { to: '/admin/users', label: 'admin.users', icon: UsersRound },
      { to: '/admin/departments', label: 'admin.departments', icon: Building2 },
      { to: '/admin/policies', label: 'admin.policies', icon: ShieldCheck },
      { to: '/admin/api-keys', label: 'admin.apiKeys', icon: KeyRound },
      { to: '/admin/usage', label: 'admin.usage', icon: WalletCards },
      { to: '/admin/cost-allocation', label: 'admin.costAllocation', icon: PieChart },
      { to: '/admin/traces', label: 'admin.traces', icon: Activity },
      { to: '/admin/alerts', label: 'admin.alerts', icon: BellRing },
      { to: '/admin/audit', label: 'admin.audit', icon: FileClock },
      { to: '/admin/exports', label: 'admin.exports', icon: FileOutput }
    ]
  },
  {
    label: 'nav.platform',
    items: [
      { to: '/admin/plugins', label: 'admin.plugins', icon: Plug },
      { to: '/admin/settings', label: 'admin.settings', icon: Settings }
    ]
  }
]
</script>

<template>
  <SurfaceShell home-to="/admin/dashboard" nav-label="nav.admin" surface="enterprise" :nav-groups="navGroups" />
</template>
