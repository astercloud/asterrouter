import { createRouter, createWebHistory } from 'vue-router'
import LoginView from '@/views/LoginView.vue'
import SetupView from '@/views/SetupView.vue'
import AdminShell from '@/views/admin/AdminShell.vue'
import AdminApiKeysView from '@/views/admin/AdminApiKeysView.vue'
import AdminAuditView from '@/views/admin/AdminAuditView.vue'
import AdminDashboardView from '@/views/admin/AdminDashboardView.vue'
import AdminExportJobsView from '@/views/admin/AdminExportJobsView.vue'
import AdminGatewayTracesView from '@/views/admin/AdminGatewayTracesView.vue'
import AdminPluginsView from '@/views/admin/AdminPluginsView.vue'
import AdminProviderAccountsView from '@/views/admin/AdminProviderAccountsView.vue'
import AdminProjectsView from '@/views/admin/AdminProjectsView.vue'
import AdminProvidersView from '@/views/admin/AdminProvidersView.vue'
import AdminRoutingGroupsView from '@/views/admin/AdminRoutingGroupsView.vue'
import AdminSettingsView from '@/views/admin/AdminSettingsView.vue'
import AdminUsageView from '@/views/admin/AdminUsageView.vue'
import PortalHomeView from '@/views/portal/PortalHomeView.vue'
import NotFoundView from '@/views/NotFoundView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/admin/dashboard' },
    { path: '/login', component: LoginView, meta: { titleKey: 'auth.signIn', descriptionKey: 'auth.signInToAccount' } },
    { path: '/setup', component: SetupView, meta: { titleKey: 'setup.title', descriptionKey: 'setup.subtitle' } },
    {
      path: '/admin',
      component: AdminShell,
      children: [
        { path: '', redirect: '/admin/dashboard' },
        { path: 'dashboard', component: AdminDashboardView, meta: { titleKey: 'admin.overview', descriptionKey: 'dashboard.subtitle' } },
        { path: 'providers', component: AdminProvidersView, meta: { titleKey: 'admin.providers', descriptionKey: 'providers.subtitle' } },
        { path: 'routing-groups', component: AdminRoutingGroupsView, meta: { titleKey: 'admin.routingGroups', descriptionKey: 'routingGroups.subtitle' } },
        { path: 'provider-accounts', component: AdminProviderAccountsView, meta: { titleKey: 'admin.providerAccounts', descriptionKey: 'providerAccounts.subtitle' } },
        { path: 'projects', component: AdminProjectsView, meta: { titleKey: 'admin.projects', descriptionKey: 'projects.subtitle' } },
        { path: 'api-keys', component: AdminApiKeysView, meta: { titleKey: 'admin.apiKeys', descriptionKey: 'apiKeys.subtitle' } },
        { path: 'usage', component: AdminUsageView, meta: { titleKey: 'admin.usage', descriptionKey: 'usage.subtitle' } },
        { path: 'traces', component: AdminGatewayTracesView, meta: { titleKey: 'admin.traces', descriptionKey: 'traces.subtitle' } },
        { path: 'exports', component: AdminExportJobsView, meta: { titleKey: 'admin.exports', descriptionKey: 'exports.subtitle' } },
        { path: 'plugins', component: AdminPluginsView, meta: { titleKey: 'admin.plugins', descriptionKey: 'plugins.subtitle' } },
        { path: 'audit', component: AdminAuditView, meta: { titleKey: 'admin.audit', descriptionKey: 'audit.subtitle' } },
        { path: 'settings', component: AdminSettingsView, meta: { titleKey: 'admin.settings', descriptionKey: 'admin.subtitle' } },
        { path: ':pathMatch(.*)*', redirect: '/admin/dashboard' }
      ]
    },
    { path: '/portal', component: PortalHomeView, meta: { titleKey: 'portal.title', descriptionKey: 'portal.subtitle' } },
    { path: '/:pathMatch(.*)*', component: NotFoundView }
  ]
})

router.beforeEach((to) => {
  const token = localStorage.getItem('asterrouter_admin_token')
  if (to.path === '/login' && token) {
    return '/admin/dashboard'
  }
  if (to.path === '/login' || to.path === '/setup') {
    return true
  }
  if ((to.path.startsWith('/admin') || to.path.startsWith('/portal')) && !token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  return true
})

export default router
