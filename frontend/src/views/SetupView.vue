<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, Building2, Check, Laptop, RadioTower, ShieldCheck } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { applySetupProfiles } from '@/api/settings'
import { ApiClientError } from '@/api/client'
import { setPublicSettingsCache } from '@/router'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const router = useRouter()
const app = useAppStore()
const selectedProfiles = ref<string[]>(['enterprise'])
const defaultProfile = ref('enterprise')
const currentStep = ref(0)
const saving = ref(false)
const error = ref('')

const profiles = [
  { id: 'enterprise', icon: Building2, title: 'setup.enterprise', desc: 'setup.enterpriseDesc', route: '/admin + /portal' },
  { id: 'personal', icon: Laptop, title: 'setup.personal', desc: 'setup.personalDesc', route: '/console/overview' },
  { id: 'relay_operator', icon: RadioTower, title: 'setup.relay', desc: 'setup.relayDesc', route: '/operator/overview' }
]

const steps = computed(() => [
  { id: 'profiles', title: t('setup.stepProfiles') },
  { id: 'entry', title: t('setup.stepEntry') },
  { id: 'ready', title: t('setup.stepReady') }
])
const selectedProfileItems = computed(() => profiles.filter((profile) => hasProfile(profile.id)))
const defaultProfileItem = computed(() => profiles.find((profile) => profile.id === defaultProfile.value) || profiles[0])
const canProceed = computed(() => selectedProfiles.value.length > 0)

function hasProfile(profile: string): boolean {
  return selectedProfiles.value.includes(profile)
}

function toggleProfile(profile: string) {
  if (hasProfile(profile)) {
    if (selectedProfiles.value.length === 1) {
      return
    }
    selectedProfiles.value = selectedProfiles.value.filter((item) => item !== profile)
    if (defaultProfile.value === profile) {
      defaultProfile.value = selectedProfiles.value[0] || ''
    }
    return
  }
  selectedProfiles.value = [...selectedProfiles.value, profile]
  if (!defaultProfile.value) {
    defaultProfile.value = profile
  }
}

function defaultRoute(): string {
  if (defaultProfile.value === 'personal') return '/console/overview'
  if (defaultProfile.value === 'relay_operator') return '/operator/overview'
  return '/admin/dashboard'
}

function nextStep() {
  if (!canProceed.value) {
    error.value = t('setup.selectAtLeastOne')
    return
  }
  error.value = ''
  if (currentStep.value < steps.value.length - 1) {
    currentStep.value += 1
  }
}

function previousStep() {
  error.value = ''
  if (currentStep.value > 0) {
    currentStep.value -= 1
  }
}

async function submit() {
  if (!selectedProfiles.value.length) {
    error.value = t('setup.selectAtLeastOne')
    return
  }
  if (!hasProfile(defaultProfile.value)) {
    defaultProfile.value = selectedProfiles.value[0]
  }
  saving.value = true
  error.value = ''
  try {
    const settings = await applySetupProfiles(selectedProfiles.value, defaultProfile.value)
    setPublicSettingsCache(settings)
    await app.loadPublicSettings()
    await router.push(defaultRoute())
  } catch (err) {
    if (err instanceof ApiClientError && (err.status === 0 || err.status === 404)) {
      error.value = t('setup.serviceUnavailable')
    } else {
      error.value = err instanceof Error ? err.message : t('common.failed')
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="setup-page">
    <main class="setup-shell">
      <section class="setup-brand">
        <span class="setup-brand-mark">
          <ShieldCheck :size="28" />
        </span>
        <h1>{{ t('setup.title') }}</h1>
        <p>{{ t('setup.subtitle') }}</p>
      </section>

      <nav class="setup-steps" :aria-label="t('setup.steps')">
        <template v-for="(step, index) in steps" :key="step.id">
          <div class="setup-step" :class="{ active: currentStep === index, done: currentStep > index }">
            <span class="setup-step-index">
              <Check v-if="currentStep > index" :size="14" />
              <span v-else>{{ index + 1 }}</span>
            </span>
            <span>{{ step.title }}</span>
          </div>
          <span v-if="index < steps.length - 1" class="setup-step-line" :class="{ done: currentStep > index }"></span>
        </template>
      </nav>

      <section class="setup-card">
        <div v-if="currentStep === 0" class="setup-step-panel">
          <div class="setup-section-header">
            <div>
              <h2>{{ t('setup.profileTitle') }}</h2>
              <p>{{ t('setup.profileHelp') }}</p>
            </div>
            <span class="pill">{{ selectedProfiles.length }} {{ t('setup.selected') }}</span>
          </div>

          <section class="setup-grid">
            <button
              v-for="profile in profiles"
              :key="profile.id"
              type="button"
              class="profile-card"
              :class="{ active: hasProfile(profile.id), primary: defaultProfile === profile.id }"
              @click="toggleProfile(profile.id)"
            >
              <span class="profile-card-topline">
                <component :is="profile.icon" :size="30" />
                <span class="profile-check" :class="{ active: hasProfile(profile.id) }">
                  <Check v-if="hasProfile(profile.id)" :size="15" />
                </span>
              </span>
              <h2>{{ t(profile.title) }}</h2>
              <p>{{ t(profile.desc) }}</p>
              <span class="profile-route">{{ profile.route }}</span>
            </button>
          </section>
        </div>

        <div v-if="currentStep === 1" class="setup-step-panel">
          <div class="setup-section-header">
            <div>
              <h2>{{ t('setup.entryTitle') }}</h2>
              <p>{{ t('setup.entryHelp') }}</p>
            </div>
          </div>

          <div class="setup-option-list">
            <label
              v-for="profile in selectedProfileItems"
              :key="profile.id"
              class="setup-option-row"
              :class="{ active: defaultProfile === profile.id }"
            >
              <input v-model="defaultProfile" type="radio" name="default_profile" :value="profile.id" />
              <span class="metric-icon">
                <component :is="profile.icon" :size="18" />
              </span>
              <span>
                <strong>{{ t(profile.title) }}</strong>
                <small>{{ profile.route }}</small>
              </span>
            </label>
          </div>
        </div>

        <div v-if="currentStep === 2" class="setup-step-panel">
          <div class="setup-section-header">
            <div>
              <h2>{{ t('setup.readyTitle') }}</h2>
              <p>{{ t('setup.readyHelp') }}</p>
            </div>
          </div>

          <div class="setup-review-grid">
            <div>
              <label>{{ t('setup.enabledProfiles') }}</label>
              <div class="chip-list">
                <span v-for="profile in selectedProfileItems" :key="profile.id" class="pill">{{ t(profile.title) }}</span>
              </div>
            </div>
            <div>
              <label>{{ t('setup.defaultProfile') }}</label>
              <strong>{{ t(defaultProfileItem.title) }}</strong>
              <span>{{ defaultProfileItem.route }}</span>
            </div>
            <div>
              <label>{{ t('setup.nextEntry') }}</label>
              <strong>{{ defaultRoute() }}</strong>
              <span>{{ t('setup.nextEntryHelp') }}</span>
            </div>
          </div>
        </div>

        <div v-if="error" class="notice setup-notice">{{ error }}</div>

        <footer class="setup-actions">
          <button v-if="currentStep > 0" class="button secondary" type="button" :disabled="saving" @click="previousStep">
            <ArrowLeft :size="17" />
            {{ t('common.previous') }}
          </button>
          <span v-else></span>

          <button
            v-if="currentStep < steps.length - 1"
            class="button"
            type="button"
            :disabled="!canProceed"
            @click="nextStep"
          >
            {{ t('common.next') }}
            <ArrowRight :size="17" />
          </button>
          <button v-else class="button" type="button" :disabled="saving || !selectedProfiles.length" @click="submit">
            <ArrowRight :size="17" />
            {{ saving ? t('common.saving') : t('setup.completeInstallation') }}
          </button>
        </footer>
      </section>
    </main>
  </div>
</template>
