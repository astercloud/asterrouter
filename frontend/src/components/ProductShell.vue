<script setup lang="ts">
import { computed, onMounted, ref, watch, type Component } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import {
  ChevronLeft,
  ChevronRight,
	ExternalLink,
	Moon,
	Puzzle,
  Sun,
  X
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import TopBar from '@/components/TopBar.vue'
import { getPluginCatalog, getPluginWorkbench } from '@/api/plugins'
import { useAppStore } from '@/stores/app'

interface ProductNavItem {
  to: string
  label: string
  icon: Component
}

interface ProductNavGroup {
  label: string
  items: ProductNavItem[]
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
    navGroups: ProductNavGroup[]
    entry: 'console' | 'portal'
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
const route = useRoute()
const collapsed = ref(localStorage.getItem(props.storageKey) === 'true')
const mobileOpen = ref(false)
const darkMode = ref(document.documentElement.dataset.theme === 'dark')
const installedPluginLinks = ref<InstalledPluginNavItem[]>([])

const version = computed(() => app.publicSettings?.version || 'Dev')
const customMenuItems = computed(() => app.publicSettings?.custom_menu_items || [])

async function loadInstalledPluginLinks() {
  if (props.entry !== 'console') return
  try {
    const catalog = await getPluginCatalog()
    const candidates = catalog.plugins.filter((plugin) =>
      plugin.status === 'enabled' &&
      plugin.packages?.some((pkg) => pkg.install_status === 'installed')
    )
    const results = await Promise.allSettled(candidates.map(async (plugin) => {
      const manifest = await getPluginWorkbench(plugin.plugin_id)
      if (!manifest.workbench?.asset) return null
      return {
        pluginID: plugin.plugin_id,
        label: manifest.workbench.title || plugin.name,
        description: plugin.description,
        to: `/console/system/plugins/${encodeURIComponent(plugin.plugin_id)}/workbench`,
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
  <div class="app-shell admin-layout" :class="[{ 'sidebar-is-collapsed': collapsed }, `entry-${entry}`]">
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
		<section v-if="installedPluginLinks.length && props.entry === 'console' && group.label === 'nav.systemManagement'" class="sidebar-section sidebar-installed-plugins" data-installed-plugin-navigation>
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
