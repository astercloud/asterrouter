<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ChevronDown, Globe2, KeyRound, LogOut, Menu, Moon, PanelsTopLeft, Sun, UserCog, UserRound } from '@lucide/vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { availableLocales, getLocale, setLocale, type LocaleCode } from '@/i18n'
import { canAccessEntry } from '@/router/access'

const props = withDefaults(defineProps<{
  showMenu?: boolean
  homeTo?: string
  entry?: 'console' | 'portal'
  brandMark?: string
}>(), {
  showMenu: false,
  homeTo: '/console/workbench',
  entry: 'console',
  brandMark: 'AR'
})

const emit = defineEmits<{ toggleMenu: [] }>()
const { t } = useI18n()
const app = useAppStore()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const accountOpen = ref(false)
const accountRef = ref<HTMLElement | null>(null)

const userInitials = computed(() => (auth.user?.display_name || auth.user?.email || auth.user?.username || 'AR').slice(0, 2).toUpperCase())
const demoMode = computed(() => Boolean(app.publicSettings?.demo_mode))
const isConsole = computed(() => route.path.startsWith('/console'))
const isPortal = computed(() => route.path.startsWith('/portal'))
const darkMode = ref(document.documentElement.dataset.theme === 'dark')

function changeLocale(event: Event) {
  setLocale((event.target as HTMLSelectElement).value as LocaleCode)
}

function toggleTheme() {
  darkMode.value = !darkMode.value
  document.documentElement.dataset.theme = darkMode.value ? 'dark' : 'light'
  localStorage.setItem('asterrouter_theme', darkMode.value ? 'dark' : 'light')
}

async function openEntry(path: string) {
  accountOpen.value = false
  await router.push(path)
}

async function openAccount() {
  accountOpen.value = false
  await router.push(isPortal.value ? '/portal/account' : '/console/account')
}

async function logout() {
  accountOpen.value = false
  try {
    await auth.signOut()
    await router.push('/login')
  } catch {
    await router.push({ path: '/login', query: { logout: 'failed' } })
  }
}

function closeOnOutsideClick(event: MouseEvent) {
  if (accountRef.value && !accountRef.value.contains(event.target as Node)) accountOpen.value = false
}

onMounted(() => {
  document.addEventListener('click', closeOnOutsideClick)
  if (auth.isAuthenticated) void auth.loadCurrentUser()
})

onBeforeUnmount(() => document.removeEventListener('click', closeOnOutsideClick))
</script>

<template>
  <header class="app-header topbar" data-global-header>
    <div class="app-header-inner">
      <div class="topbar-context">
        <button
          v-if="showMenu"
          class="icon-button mobile-menu-button"
          type="button"
          :aria-label="t('nav.openMenu')"
          :title="t('nav.openMenu')"
          @click="emit('toggleMenu')"
        >
          <Menu :size="20" />
        </button>
        <RouterLink class="global-brand-link" :to="props.homeTo">
          <img v-if="app.publicSettings?.site_logo" :src="app.publicSettings.site_logo" class="shell-brand-logo" alt="" />
          <span v-else class="brand-mark global-brand-mark">{{ props.brandMark }}</span>
          <strong>{{ app.siteName }}</strong>
        </RouterLink>
        <span class="global-header-divider" aria-hidden="true"></span>
        <span class="global-entry-label">{{ t(props.entry === 'portal' ? 'nav.portal' : 'nav.console') }}</span>
      </div>

      <div class="topbar-actions">
        <span v-if="demoMode" class="pill status-warning global-demo-status">{{ t('nav.demoMode') }}</span>
        <label class="locale-control">
          <Globe2 :size="17" aria-hidden="true" />
          <select :value="getLocale()" :aria-label="t('nav.language')" @change="changeLocale">
            <option v-for="locale in availableLocales" :key="locale.code" :value="locale.code">{{ locale.label }}</option>
          </select>
        </label>
        <button
          class="icon-button global-theme-toggle"
          type="button"
          :aria-label="darkMode ? t('nav.lightMode') : t('nav.darkMode')"
          :title="darkMode ? t('nav.lightMode') : t('nav.darkMode')"
          @click="toggleTheme"
        >
          <Sun v-if="darkMode" :size="16" />
          <Moon v-else :size="16" />
        </button>

        <div v-if="auth.user" ref="accountRef" class="account-menu">
          <button class="account-trigger" type="button" :aria-expanded="accountOpen" :aria-label="t('nav.accountMenu')" @click="accountOpen = !accountOpen">
            <span class="account-avatar">
              <img v-if="auth.user.avatar_data_url" :src="auth.user.avatar_data_url" alt="" />
              <template v-else>{{ userInitials }}</template>
            </span>
            <span class="account-copy">
              <strong>{{ auth.user.display_name || auth.user.username }}</strong>
              <small>{{ auth.user.role }}</small>
            </span>
            <ChevronDown :size="15" />
          </button>

          <div v-if="accountOpen" class="account-dropdown">
            <div class="account-dropdown-header">
              <strong>{{ auth.user.display_name || auth.user.username }}</strong>
              <span>{{ auth.user.role }}</span>
            </div>
            <button type="button" @click="openAccount">
              <UserCog :size="16" />
              {{ t('account.title') }}
            </button>
            <button v-if="!isConsole && canAccessEntry(auth.user, 'console')" type="button" @click="openEntry('/console/workbench')">
              <PanelsTopLeft :size="16" />
              {{ t('nav.console') }}
            </button>
            <button v-if="!isPortal && canAccessEntry(auth.user, 'portal')" type="button" @click="openEntry('/portal/overview')">
              <KeyRound :size="16" />
              {{ t('nav.portal') }}
            </button>
            <button class="danger-item" type="button" @click="logout">
              <LogOut :size="16" />
              {{ t('nav.logout') }}
            </button>
          </div>
        </div>
        <span v-else class="guest-avatar" aria-hidden="true"><UserRound :size="18" /></span>
      </div>
    </div>
  </header>
</template>
