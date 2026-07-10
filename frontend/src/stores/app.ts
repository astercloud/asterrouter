import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { getPublicSettings } from '@/api/settings'
import type { PublicSettings } from '@/types'

export const useAppStore = defineStore('app', () => {
  const publicSettings = ref<PublicSettings | null>(null)
  const loading = ref(false)
  const error = ref('')

  const siteName = computed(() => publicSettings.value?.site_name || 'AsterRouter')
  const siteSubtitle = computed(() => publicSettings.value?.site_subtitle || 'AI Gateway Control Plane')
  const setupCompleted = computed(() => publicSettings.value?.setup_completed ?? false)

  async function loadPublicSettings() {
    loading.value = true
    error.value = ''
    try {
      publicSettings.value = await getPublicSettings()
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Failed to load settings'
    } finally {
      loading.value = false
    }
  }

  return {
    publicSettings,
    loading,
    error,
    siteName,
    siteSubtitle,
    setupCompleted,
    loadPublicSettings
  }
})
