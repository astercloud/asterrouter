<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Code2, KeyRound, LineChart, WalletCards } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { getPortalWorkspace } from '@/api/control'
import TopBar from '@/components/TopBar.vue'
import { useAppStore } from '@/stores/app'
import type { PortalWorkspace } from '@/types'

const { t } = useI18n()
const app = useAppStore()
const loading = ref(false)
const error = ref('')
const workspace = ref<PortalWorkspace | null>(null)

const baseUrl = computed(() => {
  const settings = app.publicSettings
  const base = settings?.public_base_url || window.location.origin
  const path = workspace.value?.gateway_path || settings?.gateway_base_path || '/v1'
  return `${base.replace(/\/$/, '')}${path}`
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    workspace.value = await getPortalWorkspace()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="app-page">
    <TopBar />
    <main class="content">
      <section class="page-header">
        <div>
          <h1>{{ t('portal.title') }}</h1>
          <p>{{ t('portal.subtitle') }}</p>
        </div>
        <a class="button secondary" href="/admin/settings">{{ t('nav.admin') }}</a>
      </section>

      <div v-if="error" class="notice">{{ error }}</div>

      <section class="panel">
        <div class="panel-header">
          <Code2 :size="18" />
          <h2>{{ t('portal.gatewayBase') }}</h2>
        </div>
        <div class="panel-body">
          <input :value="baseUrl" readonly />
          <span class="hint">{{ t('portal.gatewayHelp') }}</span>
          <div class="status-line" style="margin-top:0">
            <span v-for="model in workspace?.models || []" :key="model" class="pill">{{ model }}</span>
          </div>
        </div>
      </section>

      <section class="panel" style="margin-top: 16px">
        <div class="panel-header">
          <Code2 :size="18" />
          <h2>{{ t('portal.integrationExample') }}</h2>
        </div>
        <div class="panel-body">
          <pre class="code-block">curl {{ baseUrl }}/chat/completions \
  -H "Authorization: Bearer $ASTERROUTER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}'</pre>
          <span class="hint">{{ t('portal.integrationHelp') }}</span>
        </div>
      </section>

      <section class="grid" style="margin-top: 16px">
        <div class="panel">
          <div class="panel-header"><KeyRound :size="18" /><h2>{{ t('admin.apiKeys') }}</h2></div>
          <div class="panel-body">
            <strong>{{ workspace?.api_keys.length || 0 }}</strong>
            <span class="hint">{{ t('portal.keySummary') }}</span>
          </div>
        </div>
        <div class="panel">
          <div class="panel-header"><LineChart :size="18" /><h2>{{ t('dashboard.projects') }}</h2></div>
          <div class="panel-body">
            <strong>{{ workspace?.projects.length || 0 }}</strong>
            <span class="hint">{{ t('portal.projectSummary') }}</span>
          </div>
        </div>
        <div class="panel">
          <div class="panel-header"><WalletCards :size="18" /><h2>{{ t('projects.applications') }}</h2></div>
          <div class="panel-body">
            <strong>{{ workspace?.applications.length || 0 }}</strong>
            <span class="hint">{{ loading ? t('common.loading') : t('portal.appSummary') }}</span>
          </div>
        </div>
      </section>
    </main>
  </div>
</template>
