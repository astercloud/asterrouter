<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { Eye, EyeOff, Lock, LogIn, UserRound } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { availableLocales, getLocale, setLocale, type LocaleCode } from '@/i18n'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()
const showPassword = ref(false)
const form = reactive({
  username: 'admin',
  password: ''
})

const redirectTo = computed(() => {
  const value = route.query.redirect
  return typeof value === 'string' && value.startsWith('/') ? value : '/admin/dashboard'
})

async function submit() {
  await auth.login(form.username, form.password)
  await router.push(redirectTo.value)
}

function changeLocale(event: Event) {
  setLocale((event.target as HTMLSelectElement).value as LocaleCode)
}
</script>

<template>
  <main class="auth-page">
    <div class="auth-bg-grid" aria-hidden="true"></div>
    <label class="auth-locale locale-control">
      <select :value="getLocale()" :aria-label="t('nav.language')" @change="changeLocale">
        <option v-for="locale in availableLocales" :key="locale.code" :value="locale.code">
          {{ locale.label }}
        </option>
      </select>
    </label>

    <div class="auth-container">
      <div class="auth-brand">
        <div class="brand-mark large">AR</div>
        <h1>{{ app.siteName }}</h1>
        <p>{{ app.siteSubtitle }}</p>
      </div>

      <section class="auth-card">
        <div class="auth-title">
          <h2>{{ t('auth.welcomeBack') }}</h2>
          <p>{{ t('auth.signInToAccount') }}</p>
        </div>

        <form class="auth-form" @submit.prevent="submit">
          <div class="field">
            <label for="username">{{ t('auth.username') }}</label>
            <div class="input-with-icon">
              <UserRound :size="18" aria-hidden="true" />
              <input
                id="username"
                v-model="form.username"
                autocomplete="username"
                autofocus
                required
                :placeholder="t('auth.usernamePlaceholder')"
              />
            </div>
          </div>

          <div class="field">
            <label for="password">{{ t('auth.password') }}</label>
            <div class="input-with-icon">
              <Lock :size="18" aria-hidden="true" />
              <input
                id="password"
                v-model="form.password"
                :type="showPassword ? 'text' : 'password'"
                autocomplete="current-password"
                required
                :placeholder="t('auth.passwordPlaceholder')"
              />
              <button
                type="button"
                class="icon-button"
                :aria-label="showPassword ? t('auth.hidePassword') : t('auth.showPassword')"
                :title="showPassword ? t('auth.hidePassword') : t('auth.showPassword')"
                @click="showPassword = !showPassword"
              >
                <EyeOff v-if="showPassword" :size="18" />
                <Eye v-else :size="18" />
              </button>
            </div>
          </div>

          <div v-if="auth.error" class="notice">{{ auth.error }}</div>

          <button class="button auth-submit" type="submit" :disabled="auth.loading">
            <LogIn :size="18" />
            {{ auth.loading ? t('auth.signingIn') : t('auth.signIn') }}
          </button>
        </form>
      </section>

      <p class="auth-footer">
        &copy; {{ new Date().getFullYear() }} {{ app.siteName }}. {{ t('auth.rightsReserved') }}
      </p>
    </div>
  </main>
</template>
