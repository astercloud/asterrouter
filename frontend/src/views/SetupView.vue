<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowRight, Building2, Laptop, RadioTower } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import TopBar from '@/components/TopBar.vue'
import { applySetupProfile } from '@/api/settings'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const router = useRouter()
const app = useAppStore()
const selected = ref('enterprise')
const saving = ref(false)
const error = ref('')

const profiles = [
  { id: 'enterprise', icon: Building2, title: 'setup.enterprise', desc: 'setup.enterpriseDesc' },
  { id: 'personal', icon: Laptop, title: 'setup.personal', desc: 'setup.personalDesc' },
  { id: 'relay_operator', icon: RadioTower, title: 'setup.relay', desc: 'setup.relayDesc' }
]

async function submit() {
  saving.value = true
  error.value = ''
  try {
    await applySetupProfile(selected.value)
    await app.loadPublicSettings()
    await router.push('/admin/settings')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="app-page">
    <TopBar />
    <main class="content">
      <section class="page-header">
        <div>
          <h1>{{ t('setup.title') }}</h1>
          <p>{{ t('setup.subtitle') }}</p>
        </div>
        <button class="button" :disabled="saving" @click="submit">
          <ArrowRight :size="17" />
          {{ t('setup.continue') }}
        </button>
      </section>

      <div v-if="error" class="notice">{{ error }}</div>

      <section class="setup-grid">
        <button
          v-for="profile in profiles"
          :key="profile.id"
          class="profile-card"
          :class="{ active: selected === profile.id }"
          @click="selected = profile.id"
        >
          <component :is="profile.icon" :size="30" />
          <h2>{{ t(profile.title) }}</h2>
          <p>{{ t(profile.desc) }}</p>
        </button>
      </section>
    </main>
  </div>
</template>
