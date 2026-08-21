<script setup lang="ts">
import { computed, onMounted, ref, watch, type Component } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import {
  ChevronLeft,
  ChevronRight,
	ExternalLink,
	PanelsTopLeft,
	Puzzle,
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

interface ShellTab {
  path: string
  titleKey: string
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
const router = useRouter()
const collapsed = ref(localStorage.getItem(props.storageKey) === 'true')
const mobileOpen = ref(false)
const installedPluginLinks = ref<InstalledPluginNavItem[]>([])
const openTabs = ref<ShellTab[]>([])

const customMenuItems = computed(() => app.publicSettings?.custom_menu_items || [])
const allNavItems = computed(() => [
  ...props.navGroups.flatMap((group) => group.items),
  ...installedPluginLinks.value
])

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

function tabIcon(path: string): Component {
  return allNavItems.value.find((item) => item.to === path)?.icon || PanelsTopLeft
}

async function closeTab(path: string) {
  const closingIndex = openTabs.value.findIndex((tab) => tab.path === path)
  if (closingIndex < 0) return
  const wasActive = route.path === path
  openTabs.value.splice(closingIndex, 1)
  if (!wasActive) return
  const fallback = openTabs.value[Math.max(0, closingIndex - 1)]?.path || props.homeTo
  await router.push(fallback)
}

watch(
  () => route.path,
  () => {
    mobileOpen.value = false
    if (!route.path.startsWith(`/${props.entry}`)) return
    const titleKey = typeof route.meta.titleKey === 'string' ? route.meta.titleKey : props.navLabel
    const existingTab = openTabs.value.find((tab) => tab.path === route.path)
    if (existingTab) existingTab.titleKey = titleKey
    else openTabs.value.push({ path: route.path, titleKey })
  },
  { immediate: true }
)

onMounted(() => {
  void loadInstalledPluginLinks()
})
</script>

<template>
  <div class="app-shell admin-layout" :class="[{ 'sidebar-is-collapsed': collapsed }, `entry-${entry}`]">
    <TopBar show-menu :home-to="homeTo" :entry="entry" :brand-mark="brandMark" @toggle-menu="mobileOpen = true" />

    <div class="shell-workspace">
      <aside class="sidebar admin-sidebar" :class="{ collapsed, 'mobile-open': mobileOpen }" data-shell-sidebar>
        <div class="sidebar-mobile-toolbar">
          <strong>{{ t(entry === 'portal' ? 'nav.portal' : 'nav.console') }}</strong>
          <button class="icon-button sidebar-mobile-close" type="button" :aria-label="t('nav.closeMenu')" @click="mobileOpen = false">
            <X :size="18" />
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
          <button class="sidebar-link nav-item sidebar-collapse" type="button" :title="collapsed ? t('nav.expand') : t('nav.collapse')" @click="toggleCollapsed">
            <ChevronRight v-if="collapsed" :size="19" />
            <ChevronLeft v-else :size="19" />
            <span>{{ t('nav.collapse') }}</span>
          </button>
        </div>
      </aside>

      <button v-if="mobileOpen" class="sidebar-overlay" type="button" :aria-label="t('nav.closeMenu')" @click="mobileOpen = false"></button>

      <div class="app-main admin-main">
        <nav class="shell-tabbar" :aria-label="t('nav.openTabs')" data-shell-tabs>
          <div
            v-for="tab in openTabs"
            :key="tab.path"
            class="shell-tab"
            :class="{ active: route.path === tab.path }"
          >
            <RouterLink class="shell-tab-link" :to="tab.path" :aria-current="route.path === tab.path ? 'page' : undefined">
              <component :is="tabIcon(tab.path)" :size="14" aria-hidden="true" />
              <span>{{ t(tab.titleKey) }}</span>
            </RouterLink>
            <button
              v-if="tab.path !== homeTo && openTabs.length > 1"
              class="shell-tab-close"
              type="button"
              :aria-label="t('nav.closeTab', { name: t(tab.titleKey) })"
              @click="closeTab(tab.path)"
            >
              <X :size="11" />
            </button>
          </div>
        </nav>
        <div class="shell-content-scroll">
          <RouterView />
        </div>
      </div>
    </div>
  </div>
</template>
