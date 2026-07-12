<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import {
  ChevronLeft,
  ChevronRight,
  KeyRound,
  Laptop,
  Moon,
  PanelsTopLeft,
  RadioTower,
  Sun,
  X
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import TopBar from '@/components/TopBar.vue'
import { useAppStore } from '@/stores/app'

interface SurfaceNavItem {
  to: string
  label: string
  icon: Component
}

interface SurfaceNavGroup {
  label: string
  items: SurfaceNavItem[]
}

const props = withDefaults(
  defineProps<{
    homeTo: string
    navLabel: string
    navGroups: SurfaceNavGroup[]
    surface: 'personal' | 'relay_operator' | 'enterprise' | 'portal'
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

const version = computed(() => app.publicSettings?.version || 'Dev')
const enabledProfiles = computed(() => app.publicSettings?.enabled_profiles || [])
const surfaceLinks = computed(() => {
  const links: SurfaceNavItem[] = []
  if (props.surface !== 'personal' && enabledProfiles.value.includes('personal')) {
    links.push({ to: '/console/overview', label: 'nav.console', icon: Laptop })
  }
  if (props.surface !== 'relay_operator' && enabledProfiles.value.includes('relay_operator')) {
    links.push({ to: '/operator/overview', label: 'nav.operator', icon: RadioTower })
  }
  if (enabledProfiles.value.includes('enterprise')) {
    if (props.surface !== 'enterprise') {
      links.push({ to: '/admin/dashboard', label: 'nav.admin', icon: PanelsTopLeft })
    }
    if (props.surface !== 'portal') {
      links.push({ to: '/portal/overview', label: 'nav.portal', icon: KeyRound })
    }
  }
  return links
})

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
</script>

<template>
  <div class="app-shell admin-layout" :class="{ 'sidebar-is-collapsed': collapsed }">
    <aside class="sidebar admin-sidebar" :class="{ collapsed, 'mobile-open': mobileOpen }">
      <div class="sidebar-header sidebar-brand-row">
        <RouterLink class="sidebar-brand-link" :to="homeTo">
          <span class="brand-mark">{{ brandMark }}</span>
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
        <section v-for="group in navGroups" :key="group.label" class="sidebar-section">
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
