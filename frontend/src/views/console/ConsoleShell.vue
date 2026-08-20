<script setup lang="ts">
import {
  Activity,
  Archive,
  BadgeDollarSign,
  BarChart3,
  BellRing,
  Boxes,
  Building2,
  FileClock,
  FileOutput,
  FlaskConical,
  Gauge,
  KeyRound,
  ListChecks,
  PieChart,
  Puzzle,
  RadioTower,
  Route,
  Server,
  Settings,
  ShieldCheck,
  Store,
  UsersRound
} from '@lucide/vue'
import ProductShell from '@/components/ProductShell.vue'

const navGroups = [
  {
    label: 'nav.enterpriseManagement',
    items: [
      { to: '/console/workbench', label: 'console.workbench', icon: Gauge },
      { to: '/console/applications', label: 'console.applications', icon: KeyRound },
      { to: '/console/applications/credentials', label: 'console.credentials', icon: KeyRound }
    ]
  },
  {
    label: 'nav.inference',
    items: [
      { to: '/console/model-services/catalog', label: 'supplyCatalog.title', icon: Store },
      { to: '/console/model-services', label: 'console.modelServices', icon: Boxes },
      { to: '/console/model-services/providers', label: 'admin.providers', icon: Server },
      { to: '/console/model-services/accounts', label: 'admin.providerAccounts', icon: RadioTower },
      { to: '/console/model-services/routes', label: 'admin.modelRoutes', icon: Route },
      { to: '/console/model-services/route-groups', label: 'admin.routingGroups', icon: Route },
      { to: '/console/model-services/simulator', label: 'admin.gatewaySimulator', icon: FlaskConical },
      { to: '/console/model-services/pricing', label: 'pricingRules.adminTitle', icon: BadgeDollarSign },
      { to: '/console/model-services/effective-pricing', label: 'admin.effectivePricing', icon: BadgeDollarSign }
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
    label: 'nav.analytics',
    items: [
      { to: '/console/usage', label: 'console.usageCost', icon: BarChart3 },
      { to: '/console/usage/supply', label: 'admin.supply', icon: Gauge },
      { to: '/console/usage/cost-allocation', label: 'admin.costAllocation', icon: PieChart },
      { to: '/console/usage/traces', label: 'admin.traces', icon: Activity },
      { to: '/console/usage/alerts', label: 'admin.alerts', icon: BellRing },
      { to: '/console/usage/artifacts', label: 'admin.artifacts', icon: Archive },
      { to: '/console/usage/jobs', label: 'admin.aiJobs', icon: ListChecks },
      { to: '/console/usage/exports', label: 'admin.exports', icon: FileOutput }
    ]
  },
  {
    label: 'nav.organization',
    items: [
      { to: '/console/organization', label: 'console.organization', icon: Building2 },
      { to: '/console/organization/departments', label: 'admin.departments', icon: Building2 },
      { to: '/console/organization/groups', label: 'organizationGroups.title', icon: UsersRound }
    ]
  },
  {
    label: 'nav.systemManagement',
    items: [
      { to: '/console/system/plugins', label: 'admin.plugins', icon: Puzzle },
      { to: '/console/system/audit', label: 'admin.audit', icon: FileClock },
      { to: '/console/system', label: 'console.system', icon: Settings }
    ]
  }
]
</script>

<template>
  <ProductShell home-to="/console/workbench" nav-label="nav.console" entry="console" :nav-groups="navGroups" />
</template>
