<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ChevronDown, Globe2, KeyRound, LogOut, Menu, PanelsTopLeft, UserCog, UserRound } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { availableLocales, getLocale, setLocale, type LocaleCode } from '@/i18n'
import { canAccessEntry } from '@/router/access'

withDefaults(defineProps<{ showMenu?: boolean }>(), { showMenu: false })

const emit = defineEmits<{ toggleMenu: [] }>()
const { t } = useI18n()
const app = useAppStore()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const accountOpen = ref(false)
const accountRef = ref<HTMLElement | null>(null)

const pageTitle = computed(() => typeof route.meta.titleKey === 'string' ? t(route.meta.titleKey) : app.siteName)
const pageDescription = computed(() => typeof route.meta.descriptionKey === 'string' ? t(route.meta.descriptionKey) : app.siteSubtitle)
const userInitials = computed(() => (auth.user?.display_name || auth.user?.email || auth.user?.username || 'AR').slice(0, 2).toUpperCase())
const demoMode = computed(() => Boolean(app.publicSettings?.demo_mode))
const isConsole = computed(() => route.path.startsWith('/console'))
const isPortal = computed(() => route.path.startsWith('/portal'))

function changeLocale(event: Event) {
  setLocale((event.target as HTMLSelectElement).value as LocaleCode)
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
  <header class="app-header glass topbar">
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
        <div>
          <p class="topbar-title">{{ pageTitle }}</p>
          <p class="topbar-description">{{ pageDescription }}</p>
        </div>
      </div>

      <div class="topbar-actions">
        <span v-if="demoMode" class="pill status-warning">{{ t('nav.demoMode') }}</span>
        <label class="locale-control">
          <Globe2 :size="17" aria-hidden="true" />
          <select :value="getLocale()" :aria-label="t('nav.language')" @change="changeLocale">
            <option v-for="locale in availableLocales" :key="locale.code" :value="locale.code">{{ locale.label }}</option>
          </select>
        </label>

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
