<script setup lang="ts">
import { computed, onMounted, ref, watch, type Component } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import {
  ChevronLeft,
  ChevronRight,
	ExternalLink,
	KeyRound,
	Laptop,
	Moon,
	PanelsTopLeft,
	Puzzle,
	RadioTower,
  Sun,
	UserRound,
  X
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import TopBar from '@/components/TopBar.vue'
import { getPluginCatalog, getPluginFrontendContribution } from '@/api/plugins'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { canAccessSurface } from '@/router/surfaces'

interface SurfaceNavItem {
  to: string
  label: string
  icon: Component
}

interface SurfaceNavGroup {
  label: string
  items: SurfaceNavItem[]
}

interface InstalledPluginNavItem {
  pluginID: string
  label: string
  description: string
  to: string
  icon: Component
}

const props = withDefaults(
  defineProps<{
    homeTo: string
    navLabel: string
    navGroups: SurfaceNavGroup[]
    surface: 'personal' | 'relay_operator' | 'enterprise' | 'platform' | 'portal' | 'customer'
    brandMark?: string
    storageKey?: string
  }>(),
  {
    brandMark: 'AR',
    storageKey: 'asterrouter_sidebar_collapsed'
  }
)

const { t } = useI18n()
const app = useAppStore()
const auth = useAuthStore()
const route = useRoute()
const collapsed = ref(localStorage.getItem(props.storageKey) === 'true')
const mobileOpen = ref(false)
const darkMode = ref(document.documentElement.dataset.theme === 'dark')
const installedPluginLinks = ref<InstalledPluginNavItem[]>([])

const version = computed(() => app.publicSettings?.version || 'Dev')
const enabledProfiles = computed(() => app.publicSettings?.enabled_profiles || [])
const surfaceLinks = computed(() => {
  const links: SurfaceNavItem[] = []
	if (props.surface !== 'personal' && enabledProfiles.value.includes('personal') && canAccessSurface(auth.user, 'personal')) {
    links.push({ to: '/console/overview', label: 'nav.console', icon: Laptop })
  }
	if (props.surface !== 'relay_operator' && enabledProfiles.value.includes('relay_operator') && canAccessSurface(auth.user, 'relay_operator')) {
    links.push({ to: '/operator/overview', label: 'nav.operator', icon: RadioTower })
  }
	if (props.surface !== 'customer' && enabledProfiles.value.includes('relay_operator') && canAccessSurface(auth.user, 'customer')) {
    links.push({ to: '/customer/overview', label: 'nav.customer', icon: UserRound })
  }
	if (enabledProfiles.value.includes('enterprise')) {
		if (props.surface !== 'enterprise' && canAccessSurface(auth.user, 'enterprise')) {
      links.push({ to: '/admin/dashboard', label: 'nav.admin', icon: PanelsTopLeft })
    }
		if (props.surface !== 'portal' && canAccessSurface(auth.user, 'portal')) {
      links.push({ to: '/portal/overview', label: 'nav.portal', icon: KeyRound })
    }
  }
	if (props.surface !== 'platform' && enabledProfiles.value.includes('platform') && canAccessSurface(auth.user, 'platform')) {
    links.push({ to: '/platform/overview', label: 'nav.platformConsole', icon: PanelsTopLeft })
  }
  return links
})
const customMenuItems = computed(() => app.publicSettings?.custom_menu_items || [])

async function loadInstalledPluginLinks() {
  if (props.surface !== 'personal') return
  try {
    const catalog = await getPluginCatalog()
    const candidates = catalog.plugins.filter((plugin) =>
      plugin.status === 'enabled' &&
      plugin.surfaces.includes('personal') &&
      plugin.packages?.some((pkg) => pkg.install_status === 'installed')
    )
    const results = await Promise.allSettled(candidates.map(async (plugin) => {
      const contribution = await getPluginFrontendContribution(plugin.plugin_id)
      const surface = contribution.surfaces?.find((item) => item.surface === 'console.plugins')
      if (!surface) return null
      return {
        pluginID: plugin.plugin_id,
        label: surface.title || plugin.name,
        description: plugin.description,
        to: `/console/plugins/${encodeURIComponent(plugin.plugin_id)}/workbench`,
        icon: Puzzle
      }
    }))
    installedPluginLinks.value = results
      .flatMap((result) => result.status === 'fulfilled' && result.value ? [result.value as InstalledPluginNavItem] : [])
      .sort((left, right) => left.label.localeCompare(right.label))
  } catch {
    installedPluginLinks.value = []
  }
}

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  localStorage.setItem(props.storageKey, String(collapsed.value))
}

function toggleTheme() {
  darkMode.value = !darkMode.value
  document.documentElement.dataset.theme = darkMode.value ? 'dark' : 'light'
  localStorage.setItem('asterrouter_theme', darkMode.value ? 'dark' : 'light')
}

watch(
  () => route.fullPath,
  () => {
    mobileOpen.value = false
  }
)

onMounted(() => {
  void loadInstalledPluginLinks()
})
</script>

<template>
  <div class="app-shell admin-layout" :class="[{ 'sidebar-is-collapsed': collapsed }, `surface-${surface}`]">
    <aside class="sidebar admin-sidebar" :class="{ collapsed, 'mobile-open': mobileOpen }">
      <div class="sidebar-header sidebar-brand-row">
        <RouterLink class="sidebar-brand-link" :to="homeTo">
		  <img v-if="app.publicSettings?.site_logo" :src="app.publicSettings.site_logo" class="shell-brand-logo" alt=""/>
		  <span v-else class="brand-mark">{{ brandMark }}</span>
          <span class="sidebar-brand-copy">
            <strong>{{ app.siteName }}</strong>
            <small>v{{ version }}</small>
          </span>
        </RouterLink>
        <button class="icon-button sidebar-mobile-close" type="button" :aria-label="t('nav.closeMenu')" @click="mobileOpen = false">
          <X :size="19" />
        </button>
      </div>

      <nav class="sidebar-nav" :aria-label="t(navLabel)">
		<template v-for="group in navGroups" :key="group.label">
		<section v-if="installedPluginLinks.length && props.surface === 'personal' && group.label === 'nav.inference'" class="sidebar-section sidebar-installed-plugins" data-installed-plugin-navigation>
		  <p class="sidebar-section-title">
		    <span>{{ t('nav.installedPlugins') }}</span>
		    <span class="sidebar-plugin-count" aria-hidden="true">{{ installedPluginLinks.length }}</span>
		  </p>
		  <RouterLink
		    v-for="link in installedPluginLinks"
		    :key="link.pluginID"
		    class="sidebar-link nav-item sidebar-plugin-link"
		    :to="link.to"
		    :title="collapsed ? link.description || link.label : undefined"
		  >
		    <component :is="link.icon" :size="19" />
		    <span>{{ link.label }}</span>
		  </RouterLink>
		</section>
		<section class="sidebar-section">
          <p class="sidebar-section-title">{{ t(group.label) }}</p>
          <RouterLink
            v-for="item in group.items"
            :key="item.to"
            class="sidebar-link nav-item"
            :to="item.to"
            :title="collapsed ? t(item.label) : undefined"
          >
            <component :is="item.icon" :size="19" />
            <span>{{ t(item.label) }}</span>
          </RouterLink>
		</section>
		</template>
		<section v-if="surfaceLinks.length" class="sidebar-section sidebar-workspaces">
          <p class="sidebar-section-title">{{ t('nav.workspaces') }}</p>
          <RouterLink
            v-for="link in surfaceLinks"
            :key="link.to"
            class="sidebar-link nav-item"
            :to="link.to"
            :title="collapsed ? t(link.label) : undefined"
          >
            <component :is="link.icon" :size="19" />
            <span>{{ t(link.label) }}</span>
          </RouterLink>
        </section>
		<section v-if="customMenuItems.length" class="sidebar-section"><p class="sidebar-section-title">企业链接</p><template v-for="item in customMenuItems" :key="item.id"><RouterLink v-if="item.url.startsWith('/') && !item.open_in_new_tab" class="sidebar-link nav-item" :to="item.url"><ExternalLink :size="19"/><span>{{ item.label }}</span></RouterLink><a v-else class="sidebar-link nav-item" :href="item.url" :target="item.open_in_new_tab?'_blank':undefined" :rel="item.open_in_new_tab?'noopener noreferrer':undefined"><ExternalLink :size="19"/><span>{{ item.label }}</span></a></template></section>
      </nav>

      <div class="app-sidebar-footer sidebar-footer">
        <button class="sidebar-link nav-item" type="button" :title="darkMode ? t('nav.lightMode') : t('nav.darkMode')" @click="toggleTheme">
          <Sun v-if="darkMode" :size="19" />
          <Moon v-else :size="19" />
          <span>{{ darkMode ? t('nav.lightMode') : t('nav.darkMode') }}</span>
        </button>
        <button class="sidebar-link nav-item sidebar-collapse" type="button" :title="collapsed ? t('nav.expand') : t('nav.collapse')" @click="toggleCollapsed">
          <ChevronRight v-if="collapsed" :size="19" />
          <ChevronLeft v-else :size="19" />
          <span>{{ t('nav.collapse') }}</span>
        </button>
      </div>
    </aside>

    <button v-if="mobileOpen" class="sidebar-overlay" type="button" :aria-label="t('nav.closeMenu')" @click="mobileOpen = false"></button>

    <div class="app-main admin-main">
      <TopBar show-menu @toggle-menu="mobileOpen = true" />
      <RouterView />
    </div>
  </div>
</template>
