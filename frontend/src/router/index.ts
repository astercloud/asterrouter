import { createRouter, createWebHistory } from 'vue-router'
import { getPublicSettings } from '@/api/settings'
import type { AuthUser, PublicSettings } from '@/types'
import { canAccessEntry, entryForUser, productEntryForPath } from './access'

const MarketingHomeView = () => import('@/views/MarketingHomeView.vue')
const LoginView = () => import('@/views/LoginView.vue')
const LegalDocumentView = () => import('@/views/LegalDocumentView.vue')
const AccountProfileView = () => import('@/views/AccountProfileView.vue')
const SetupView = () => import('@/views/SetupView.vue')
const ConsoleShell = () => import('@/views/console/ConsoleShell.vue')
const PortalShell = () => import('@/views/portal/PortalShell.vue')

const AdminOnboardingView = () => import('@/views/admin/AdminOnboardingView.vue')
const AdminApiKeysView = () => import('@/views/admin/AdminApiKeysView.vue')
const AdminAlertsView = () => import('@/views/admin/AdminAlertsView.vue')
const AdminArtifactsView = () => import('@/views/admin/AdminArtifactsView.vue')
const AdminAIJobsView = () => import('@/views/admin/AdminAIJobsView.vue')
const AdminAuditView = () => import('@/views/admin/AdminAuditView.vue')
const AdminCostAllocationView = () => import('@/views/admin/AdminCostAllocationView.vue')
const AdminDashboardView = () => import('@/views/admin/AdminDashboardView.vue')
const AdminDepartmentsView = () => import('@/views/admin/AdminDepartmentsView.vue')
const AdminEffectivePricingView = () => import('@/views/admin/AdminEffectivePricingView.vue')
const AdminSupplyView = () => import('@/views/admin/AdminSupplyView.vue')
const AdminOrganizationGroupsView = () => import('@/views/admin/AdminOrganizationGroupsView.vue')
const AdminExportJobsView = () => import('@/views/admin/AdminExportJobsView.vue')
const AdminGatewayTracesView = () => import('@/views/admin/AdminGatewayTracesView.vue')
const AdminGatewayModelsView = () => import('@/views/admin/AdminGatewayModelsView.vue')
const AdminGatewaySimulatorView = () => import('@/views/admin/AdminGatewaySimulatorView.vue')
const AdminPricingView = () => import('@/views/admin/AdminPricingView.vue')
const AdminModelRoutesView = () => import('@/views/admin/AdminModelRoutesView.vue')
const AdminPluginsView = () => import('@/views/admin/AdminPluginsView.vue')
const PluginFrontendView = () => import('@/views/admin/PluginFrontendView.vue')
const AdminPoliciesView = () => import('@/views/admin/AdminPoliciesView.vue')
const AdminProviderAccountsView = () => import('@/views/admin/AdminProviderAccountsView.vue')
const AdminProvidersView = () => import('@/views/admin/AdminProvidersView.vue')
const AdminRoutingGroupsView = () => import('@/views/admin/AdminRoutingGroupsView.vue')
const AdminRoutingPolicyView = () => import('@/views/admin/AdminRoutingPolicyView.vue')
const AdminSettingsView = () => import('@/views/admin/AdminSettingsView.vue')
const AdminUsageView = () => import('@/views/admin/AdminUsageView.vue')
const AdminUsersView = () => import('@/views/admin/AdminUsersView.vue')
const PortalHomeView = () => import('@/views/portal/PortalHomeView.vue')
const PortalIntegrationView = () => import('@/views/portal/PortalIntegrationView.vue')
const PortalKeysView = () => import('@/views/portal/PortalKeysView.vue')

let publicSettingsCache: PublicSettings | null = null

export function setPublicSettingsCache(settings: PublicSettings | null) {
  publicSettingsCache = settings?.setup_completed ? settings : null
}

export function clearPublicSettingsCache() {
  publicSettingsCache = null
}

async function loadPublicSettings(): Promise<PublicSettings | null> {
  if (publicSettingsCache) return publicSettingsCache
  try {
    const settings = await getPublicSettings()
    setPublicSettingsCache(settings)
    return settings
  } catch {
    return null
  }
}

function storedUser(): AuthUser | null {
  try {
    return JSON.parse(localStorage.getItem('asterrouter_admin_user') || 'null') as AuthUser | null
  } catch {
    return null
  }
}

function defaultEntry(settings: PublicSettings | null): string {
  if (!settings?.setup_completed) return '/setup'
  return entryForUser(storedUser())
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: MarketingHomeView, meta: { titleKey: 'marketing.metaTitle', descriptionKey: 'marketing.metaDescription' } },
    { path: '/login', component: LoginView, meta: { titleKey: 'auth.signIn', descriptionKey: 'auth.signInToAccount' } },
    { path: '/register', component: LoginView, meta: { titleKey: 'auth.createAccount', descriptionKey: 'auth.registrationHelp' } },
    { path: '/forgot-password', component: LoginView, meta: { titleKey: 'auth.forgotPassword', descriptionKey: 'auth.resetEmailHelp' } },
    { path: '/resend-verification', component: LoginView, meta: { titleKey: 'auth.resendVerification', descriptionKey: 'auth.resendVerificationHelp' } },
    { path: '/reset-password', component: LoginView, meta: { titleKey: 'auth.resetPassword', descriptionKey: 'auth.resetPasswordHelp' } },
    { path: '/verify-email', component: LoginView, meta: { titleKey: 'auth.verifyEmail', descriptionKey: 'auth.verifyEmailHelp' } },
    { path: '/setup', component: SetupView, meta: { titleKey: 'setup.title', descriptionKey: 'setup.subtitle' } },
    {
      path: '/console',
      component: ConsoleShell,
      children: [
        { path: '', redirect: '/console/workbench' },
        { path: 'workbench', component: AdminDashboardView, meta: { titleKey: 'console.workbench', descriptionKey: 'console.workbenchSubtitle' } },
        { path: 'applications', component: AdminOnboardingView, meta: { titleKey: 'console.applications', descriptionKey: 'console.applicationsSubtitle' } },
        { path: 'applications/credentials', component: AdminApiKeysView, meta: { titleKey: 'console.credentials', descriptionKey: 'apiKeys.subtitle' } },
        { path: 'model-services', component: AdminGatewayModelsView, meta: { titleKey: 'console.modelServices', descriptionKey: 'console.modelServicesSubtitle' } },
        { path: 'model-services/providers', component: AdminProvidersView, meta: { titleKey: 'admin.providers', descriptionKey: 'providers.subtitle' } },
        { path: 'model-services/accounts', component: AdminProviderAccountsView, meta: { titleKey: 'admin.providerAccounts', descriptionKey: 'providerAccounts.subtitle' } },
        { path: 'model-services/routes', component: AdminModelRoutesView, meta: { titleKey: 'admin.modelRoutes', descriptionKey: 'modelRoutes.subtitle' } },
        { path: 'model-services/route-groups', component: AdminRoutingGroupsView, meta: { titleKey: 'admin.routingGroups', descriptionKey: 'routingGroups.subtitle' } },
        { path: 'model-services/simulator', component: AdminGatewaySimulatorView, meta: { titleKey: 'admin.gatewaySimulator', descriptionKey: 'gatewaySimulator.subtitle' } },
        { path: 'model-services/pricing', component: AdminPricingView, meta: { titleKey: 'pricingRules.adminTitle', descriptionKey: 'pricingRules.adminSubtitle' } },
        { path: 'model-services/effective-pricing', component: AdminEffectivePricingView, meta: { titleKey: 'admin.effectivePricing', descriptionKey: 'effectivePricing.subtitle' } },
        { path: 'policies/access', component: AdminPoliciesView, meta: { titleKey: 'policy.access.title', descriptionKey: 'policy.access.subtitle' } },
        { path: 'policies/routing', component: AdminRoutingPolicyView, meta: { titleKey: 'policy.routing.title', descriptionKey: 'policy.routing.subtitle' } },
        { path: 'usage', component: AdminUsageView, meta: { titleKey: 'console.usageCost', descriptionKey: 'console.usageCostSubtitle' } },
        { path: 'usage/supply', component: AdminSupplyView, meta: { titleKey: 'admin.supply', descriptionKey: 'supply.subtitle' } },
        { path: 'usage/cost-allocation', component: AdminCostAllocationView, meta: { titleKey: 'admin.costAllocation', descriptionKey: 'costAllocation.subtitle' } },
        { path: 'usage/traces', component: AdminGatewayTracesView, meta: { titleKey: 'admin.traces', descriptionKey: 'traces.subtitle' } },
        { path: 'usage/alerts', component: AdminAlertsView, meta: { titleKey: 'admin.alerts', descriptionKey: 'alerts.subtitle' } },
        { path: 'usage/artifacts', component: AdminArtifactsView, meta: { titleKey: 'admin.artifacts', descriptionKey: 'artifactOps.subtitle' } },
        { path: 'usage/jobs', component: AdminAIJobsView, meta: { titleKey: 'admin.aiJobs', descriptionKey: 'aiJobOps.subtitle' } },
        { path: 'usage/exports', component: AdminExportJobsView, meta: { titleKey: 'admin.exports', descriptionKey: 'exports.subtitle' } },
        { path: 'organization', component: AdminUsersView, meta: { titleKey: 'console.organization', descriptionKey: 'console.organizationSubtitle' } },
        { path: 'organization/departments', component: AdminDepartmentsView, meta: { titleKey: 'admin.departments', descriptionKey: 'departments.subtitle' } },
        { path: 'organization/groups', component: AdminOrganizationGroupsView, meta: { titleKey: 'organizationGroups.title', descriptionKey: 'organizationGroups.subtitle' } },
        { path: 'plugins', redirect: '/console/system/plugins' },
        { path: 'plugins/:pluginId/workbench', redirect: (to) => `/console/system/plugins/${encodeURIComponent(String(to.params.pluginId || ''))}/workbench` },
        { path: 'system', component: AdminSettingsView, meta: { titleKey: 'console.system', descriptionKey: 'console.systemSubtitle' } },
        { path: 'system/plugins', component: AdminPluginsView, meta: { titleKey: 'admin.plugins', descriptionKey: 'plugins.subtitle' } },
        { path: 'system/plugins/:pluginId/workbench', component: PluginFrontendView, meta: { titleKey: 'admin.plugins', descriptionKey: 'plugins.subtitle' } },
        { path: 'system/audit', component: AdminAuditView, meta: { titleKey: 'admin.audit', descriptionKey: 'audit.subtitle' } },
        { path: 'account', component: AccountProfileView, meta: { titleKey: 'account.title', descriptionKey: 'account.subtitle' } },
        { path: ':pathMatch(.*)*', redirect: '/console/workbench' }
      ]
    },
    {
      path: '/portal',
      component: PortalShell,
      children: [
        { path: '', redirect: '/portal/overview' },
        { path: 'overview', component: PortalHomeView, meta: { titleKey: 'portal.overview', descriptionKey: 'portal.subtitle', portalPanel: 'overview' } },
        { path: 'applications', component: PortalKeysView, meta: { titleKey: 'portal.applications', descriptionKey: 'portal.applicationsSubtitle' } },
        { path: 'access', component: PortalIntegrationView, meta: { titleKey: 'portal.access', descriptionKey: 'portal.accessSubtitle' } },
        { path: 'usage', component: PortalHomeView, meta: { titleKey: 'portal.usage', descriptionKey: 'portal.usageHelp', portalPanel: 'usage' } },
        { path: 'account', component: AccountProfileView, meta: { titleKey: 'account.title', descriptionKey: 'account.subtitle' } },
        { path: ':pathMatch(.*)*', redirect: '/portal/overview' }
      ]
    },
    { path: '/legal/:slug', component: LegalDocumentView },
    { path: '/:pathMatch(.*)*', redirect: '/' }
  ]
})

router.beforeEach(async (to) => {
  const token = localStorage.getItem('asterrouter_admin_token')
  if (to.path === '/') return true
  const settings = await loadPublicSettings()
  const entry = defaultEntry(settings)

  const publicAuthPath = ['/login', '/register', '/forgot-password', '/resend-verification', '/reset-password', '/verify-email'].includes(to.path)
  if (publicAuthPath) {
    if (!settings?.setup_completed) return '/setup'
    if (token && ['/login', '/register', '/forgot-password', '/resend-verification'].includes(to.path)) return entry
    return true
  }
  if (to.path === '/setup') return settings?.setup_completed ? entry : true
  if (to.path.startsWith('/legal/')) return true
  if (!settings?.setup_completed) return '/setup'

  const targetEntry = productEntryForPath(to.path)
  if (targetEntry && !token) return { path: '/login', query: { redirect: to.fullPath } }
  const user = storedUser()
  if (user && targetEntry && !canAccessEntry(user, targetEntry)) return entry
  return true
})

export default router
