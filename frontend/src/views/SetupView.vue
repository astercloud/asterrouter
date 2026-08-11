<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowRight, Building2, ShieldCheck } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { completeEnterpriseSetup } from '@/api/settings'
import { ApiClientError } from '@/api/client'
import { setPublicSettingsCache } from '@/router'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()
const organizationName = ref('')
const saving = ref(false)
const error = ref('')

async function submit() {
  const name = organizationName.value.trim()
  if (!name) {
    error.value = t('setup.organizationRequired')
    return
  }
  saving.value = true
  error.value = ''
  try {
    const settings = await completeEnterpriseSetup(name)
    setPublicSettingsCache(settings)
    await app.loadPublicSettings()
    auth.logout()
    await router.push({ path: '/login', query: { redirect: '/console/workbench' } })
  } catch (err) {
    if (err instanceof ApiClientError && (err.status === 0 || err.status === 404)) error.value = t('setup.serviceUnavailable')
    else error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="setup-page">
    <main class="setup-shell">
      <section class="setup-brand">
        <span class="setup-brand-mark"><ShieldCheck :size="28" /></span>
        <h1>{{ t('setup.title') }}</h1>
        <p>{{ t('setup.subtitle') }}</p>
      </section>

      <section class="setup-card setup-step-panel">
        <div class="setup-section-header">
          <div>
            <h2>{{ t('setup.organizationTitle') }}</h2>
            <p>{{ t('setup.organizationHelp') }}</p>
          </div>
          <span class="pill"><Building2 :size="15" /> {{ t('setup.enterpriseInstance') }}</span>
        </div>

        <label class="field">
          <span>{{ t('setup.organizationName') }}</span>
          <input v-model="organizationName" autocomplete="organization" :placeholder="t('setup.organizationPlaceholder')" @keyup.enter="submit" />
        </label>

        <div class="setup-review-boundaries">
          <section>
            <h3>{{ t('setup.afterSetup') }}</h3>
            <ul>
              <li>{{ t('setup.afterSetup1') }}</li>
              <li>{{ t('setup.afterSetup2') }}</li>
              <li>{{ t('setup.afterSetup3') }}</li>
            </ul>
          </section>
        </div>

        <div v-if="error" class="notice setup-notice">{{ error }}</div>
        <footer class="setup-actions">
          <span></span>
          <button class="button" type="button" :disabled="saving || !organizationName.trim()" @click="submit">
            <ArrowRight :size="17" />
            {{ saving ? t('common.saving') : t('setup.completeInstallation') }}
          </button>
        </footer>
      </section>
    </main>
  </div>
</template>
