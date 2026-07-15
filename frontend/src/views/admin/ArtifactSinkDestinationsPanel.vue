<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { Edit3, Plus, RefreshCw, Save, Trash2, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { deleteArtifactSinkDestination, getArtifactSinkDestinations, upsertArtifactSinkDestination } from '@/api/plugins'
import type { ArtifactSinkDestination, ArtifactSinkDestinationRequest, ArtifactSinkProvider } from '@/types'

const props = defineProps<{ pluginId: string }>()
const { t } = useI18n()
const destinations = ref<ArtifactSinkDestination[]>([])
const loading = ref(false)
const saving = ref(false)
const deletingID = ref('')
const error = ref('')
const message = ref('')
const editorOpen = ref(false)
const editingID = ref('')
const firstInput = ref<HTMLInputElement | null>(null)

type DestinationForm = {
  id: string
  name: string
  provider: ArtifactSinkProvider
  endpoint: string
  region: string
  bucket: string
  prefix: string
  referenceBaseURL: string
  allowedProfileScope: string
  allowedTenantID: string
  pathStyle: boolean
  enabled: boolean
  accessKey: string
  secretKey: string
  sessionToken: string
  clearSessionToken: boolean
}

const form = reactive<DestinationForm>(blankForm())
const editingDestination = computed(() => destinations.value.find((item) => item.id === editingID.value) || null)

function blankForm(): DestinationForm {
  return {
    id: '', name: '', provider: 's3', endpoint: '', region: 'us-east-1', bucket: '', prefix: '', referenceBaseURL: '',
    allowedProfileScope: '', allowedTenantID: '', pathStyle: false, enabled: true,
    accessKey: '', secretKey: '', sessionToken: '', clearSessionToken: false
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    destinations.value = await getArtifactSinkDestinations(props.pluginId)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function openCreate() {
  Object.assign(form, blankForm())
  editingID.value = ''
  editorOpen.value = true
  await nextTick()
  firstInput.value?.focus()
}

async function openEdit(destination: ArtifactSinkDestination) {
  Object.assign(form, {
    id: destination.id,
    name: destination.name,
    provider: destination.provider,
    endpoint: destination.endpoint || '',
    region: destination.region,
    bucket: destination.bucket,
    prefix: destination.prefix || '',
    referenceBaseURL: destination.reference_base_url || '',
    allowedProfileScope: destination.allowed_profile_scope || '',
    allowedTenantID: destination.allowed_tenant_id || '',
    pathStyle: destination.path_style,
    enabled: destination.enabled,
    accessKey: '',
    secretKey: '',
    sessionToken: '',
    clearSessionToken: false
  })
  editingID.value = destination.id
  editorOpen.value = true
  await nextTick()
  firstInput.value?.focus()
}

function closeEditor() {
  editorOpen.value = false
  editingID.value = ''
  Object.assign(form, blankForm())
}

async function saveDestination() {
  const sinkID = form.id.trim()
  if (!sinkID) return
  saving.value = true
  error.value = ''
  message.value = ''
  const secrets: Record<string, string> = {}
  if (form.accessKey.trim()) secrets.access_key = form.accessKey.trim()
  if (form.secretKey.trim()) secrets.secret_key = form.secretKey.trim()
  if (form.sessionToken.trim()) secrets.session_token = form.sessionToken.trim()
  const payload: ArtifactSinkDestinationRequest = {
    name: form.name.trim(),
    provider: form.provider,
    endpoint: form.endpoint.trim(),
    region: form.region.trim(),
    bucket: form.bucket.trim(),
    prefix: form.prefix.trim(),
    reference_base_url: form.referenceBaseURL.trim(),
    allowed_profile_scope: form.allowedProfileScope,
    allowed_tenant_id: form.allowedTenantID.trim(),
    path_style: form.pathStyle,
    enabled: form.enabled,
    secrets,
    clear_session_token: form.clearSessionToken
  }
  try {
    await upsertArtifactSinkDestination(props.pluginId, sinkID, payload)
    form.accessKey = ''
    form.secretKey = ''
    form.sessionToken = ''
    await load()
    closeEditor()
    message.value = t('plugins.artifactSinkSaved')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function removeDestination(destination: ArtifactSinkDestination) {
  if (!window.confirm(t('plugins.artifactSinkDeleteConfirm', { name: destination.name }))) return
  deletingID.value = destination.id
  error.value = ''
  message.value = ''
  try {
    await deleteArtifactSinkDestination(props.pluginId, destination.id)
    destinations.value = destinations.value.filter((item) => item.id !== destination.id)
    message.value = t('plugins.artifactSinkDeleted')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    deletingID.value = ''
  }
}

function providerLabel(provider: ArtifactSinkProvider): string {
  return t(`plugins.artifactSinkProviders.${provider}`)
}

function ownerLabel(destination: ArtifactSinkDestination): string {
  const profile = destination.allowed_profile_scope || t('plugins.artifactSinkAnyProfile')
  return destination.allowed_tenant_id ? `${profile} / ${destination.allowed_tenant_id}` : profile
}

watch(() => props.pluginId, load)
onMounted(load)
</script>

<template>
  <section class="plugin-detail-section artifact-sink-section" data-artifact-sinks>
    <div class="plugin-section-title artifact-sink-title">
      <div>
        <h3>{{ t('plugins.artifactSinks') }}</h3>
        <span class="artifact-sink-count">{{ destinations.length }}</span>
      </div>
      <div class="artifact-sink-actions">
        <button class="icon-button" type="button" :disabled="loading" :aria-label="t('common.refresh')" :title="t('common.refresh')" @click="load">
          <RefreshCw :size="15" />
        </button>
        <button class="button secondary tiny-button" type="button" @click="openCreate">
          <Plus :size="15" />
          {{ t('plugins.artifactSinkAdd') }}
        </button>
      </div>
    </div>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <div class="artifact-sink-list" role="table" :aria-label="t('plugins.artifactSinks')">
      <div class="artifact-sink-row artifact-sink-header" role="row">
        <span role="columnheader">{{ t('plugins.artifactSinkDestination') }}</span>
        <span role="columnheader">{{ t('plugins.artifactSinkProvider') }}</span>
        <span role="columnheader">{{ t('plugins.artifactSinkStorage') }}</span>
        <span role="columnheader">{{ t('plugins.artifactSinkOwner') }}</span>
        <span role="columnheader">{{ t('plugins.status') }}</span>
        <span role="columnheader">{{ t('common.actions') }}</span>
      </div>
      <div v-for="destination in destinations" :key="destination.id" class="artifact-sink-row" role="row">
        <div class="artifact-sink-main" role="cell">
          <strong>{{ destination.name }}</strong>
          <small>{{ destination.id }}</small>
        </div>
        <div role="cell">
          <span class="provider-mark" :class="`provider-${destination.provider}`" aria-hidden="true" />
          <span>{{ providerLabel(destination.provider) }}</span>
        </div>
        <div class="artifact-sink-main" role="cell">
          <strong>{{ destination.bucket }}</strong>
          <small>{{ destination.region }}<template v-if="destination.prefix"> / {{ destination.prefix }}</template></small>
        </div>
        <div class="artifact-sink-owner" role="cell">{{ ownerLabel(destination) }}</div>
        <div role="cell">
          <span class="pill" :class="destination.enabled ? 'status-success' : 'status-warning'">
            {{ destination.enabled ? t('plugins.enabled') : t('plugins.artifactSinkDisabled') }}
          </span>
        </div>
        <div class="artifact-sink-row-actions" role="cell">
          <button class="icon-button" type="button" :aria-label="t('plugins.artifactSinkEdit', { name: destination.name })" :title="t('common.edit')" @click="openEdit(destination)">
            <Edit3 :size="15" />
          </button>
          <button class="icon-button danger-icon" type="button" :disabled="deletingID === destination.id" :aria-label="t('plugins.artifactSinkDelete', { name: destination.name })" :title="t('plugins.artifactSinkDeleteAction')" @click="removeDestination(destination)">
            <Trash2 :size="15" />
          </button>
        </div>
      </div>
      <div v-if="!destinations.length" class="artifact-sink-empty">
        {{ loading ? t('common.loading') : t('plugins.artifactSinkEmpty') }}
      </div>
    </div>

    <div v-if="editorOpen" class="modal-backdrop" @click.self="closeEditor" @keydown.esc="closeEditor">
      <form class="modal-card modal-card-wide artifact-sink-modal" role="dialog" aria-modal="true" aria-labelledby="artifact-sink-dialog-title" @submit.prevent="saveDestination">
        <header class="modal-header">
          <div>
            <h2 id="artifact-sink-dialog-title">{{ editingID ? t('plugins.artifactSinkEditTitle') : t('plugins.artifactSinkCreateTitle') }}</h2>
            <p>{{ t('plugins.artifactSinkDialogSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeEditor"><X :size="18" /></button>
        </header>
        <div class="modal-body form-grid">
          <div class="field">
            <label for="artifact-sink-id">{{ t('plugins.artifactSinkID') }}</label>
            <input id="artifact-sink-id" ref="firstInput" v-model="form.id" required maxlength="160" :disabled="Boolean(editingID)" autocomplete="off" />
          </div>
          <div class="field">
            <label for="artifact-sink-name">{{ t('plugins.artifactSinkName') }}</label>
            <input id="artifact-sink-name" v-model="form.name" required maxlength="160" autocomplete="off" />
          </div>
          <div class="field">
            <label for="artifact-sink-provider">{{ t('plugins.artifactSinkProvider') }}</label>
            <select id="artifact-sink-provider" v-model="form.provider">
              <option value="s3">{{ providerLabel('s3') }}</option>
              <option value="r2">{{ providerLabel('r2') }}</option>
              <option value="oss">{{ providerLabel('oss') }}</option>
            </select>
          </div>
          <div class="field">
            <label for="artifact-sink-region">{{ t('plugins.artifactSinkRegion') }}</label>
            <input id="artifact-sink-region" v-model="form.region" required autocomplete="off" />
          </div>
          <div class="field form-span-2">
            <label for="artifact-sink-endpoint">{{ t('plugins.artifactSinkEndpoint') }}</label>
            <input id="artifact-sink-endpoint" v-model="form.endpoint" type="url" :required="form.provider !== 's3'" placeholder="https://" autocomplete="off" />
          </div>
          <div class="field">
            <label for="artifact-sink-bucket">{{ t('plugins.artifactSinkBucket') }}</label>
            <input id="artifact-sink-bucket" v-model="form.bucket" required maxlength="255" autocomplete="off" />
          </div>
          <div class="field">
            <label for="artifact-sink-prefix">{{ t('plugins.artifactSinkPrefix') }}</label>
            <input id="artifact-sink-prefix" v-model="form.prefix" autocomplete="off" />
          </div>
          <div class="field form-span-2">
            <label for="artifact-sink-reference">{{ t('plugins.artifactSinkReferenceURL') }}</label>
            <input id="artifact-sink-reference" v-model="form.referenceBaseURL" type="url" placeholder="https://" autocomplete="off" />
          </div>
          <div class="field">
            <label for="artifact-sink-profile">{{ t('plugins.artifactSinkProfileScope') }}</label>
            <select id="artifact-sink-profile" v-model="form.allowedProfileScope">
              <option value="">{{ t('plugins.artifactSinkAnyProfile') }}</option>
              <option value="personal">personal</option>
              <option value="relay_operator">relay_operator</option>
              <option value="enterprise">enterprise</option>
              <option value="platform">platform</option>
            </select>
          </div>
          <div class="field">
            <label for="artifact-sink-tenant">{{ t('plugins.artifactSinkTenant') }}</label>
            <input id="artifact-sink-tenant" v-model="form.allowedTenantID" :placeholder="t('plugins.artifactSinkAnyTenant')" autocomplete="off" />
          </div>
          <div class="field">
            <label for="artifact-sink-access-key">{{ t('plugins.artifactSinkAccessKey') }}</label>
            <input id="artifact-sink-access-key" v-model="form.accessKey" type="password" :required="!editingDestination?.secret_hints.access_key" :placeholder="editingDestination?.secret_hints.access_key || ''" autocomplete="new-password" />
          </div>
          <div class="field">
            <label for="artifact-sink-secret-key">{{ t('plugins.artifactSinkSecretKey') }}</label>
            <input id="artifact-sink-secret-key" v-model="form.secretKey" type="password" :required="!editingDestination?.secret_hints.secret_key" :placeholder="editingDestination?.secret_hints.secret_key || ''" autocomplete="new-password" />
          </div>
          <div class="field form-span-2">
            <label for="artifact-sink-session-token">{{ t('plugins.artifactSinkSessionToken') }}</label>
            <input id="artifact-sink-session-token" v-model="form.sessionToken" type="password" :placeholder="editingDestination?.secret_hints.session_token || ''" autocomplete="new-password" />
          </div>
          <label v-if="editingDestination?.secret_hints.session_token" class="checkbox-row form-span-2">
            <input v-model="form.clearSessionToken" type="checkbox" />
            <span>{{ t('plugins.artifactSinkClearSessionToken') }}</span>
          </label>
          <label class="checkbox-row">
            <input v-model="form.pathStyle" type="checkbox" />
            <span>{{ t('plugins.artifactSinkPathStyle') }}</span>
          </label>
          <label class="checkbox-row">
            <input v-model="form.enabled" type="checkbox" />
            <span>{{ t('plugins.artifactSinkEnabled') }}</span>
          </label>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeEditor">{{ t('common.cancel') }}</button>
          <button class="button" type="submit" :disabled="saving">
            <Save :size="16" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </form>
    </div>
  </section>
</template>

<style scoped>
.artifact-sink-section {
  min-width: 0;
}

.artifact-sink-title > div,
.artifact-sink-actions,
.artifact-sink-row > div,
.artifact-sink-row-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.artifact-sink-count {
  display: grid;
  min-width: 22px;
  min-height: 22px;
  place-items: center;
  border-radius: 50%;
  background: var(--surface-subtle);
  color: var(--text-secondary);
  font-size: 11px;
  font-weight: 750;
}

.artifact-sink-list {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
}

.artifact-sink-row {
  display: grid;
  grid-template-columns: minmax(150px, 1.15fr) minmax(90px, 0.55fr) minmax(150px, 1fr) minmax(130px, 0.9fr) minmax(82px, auto) 76px;
  min-height: 62px;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-top: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: 12px;
}

.artifact-sink-row:first-child {
  border-top: 0;
}

.artifact-sink-header {
  min-height: 38px;
  background: var(--surface-subtle);
  color: var(--text-muted);
  font-size: 10px;
  font-weight: 750;
  text-transform: uppercase;
}

.artifact-sink-main {
  display: grid !important;
  min-width: 0;
  gap: 3px !important;
}

.artifact-sink-main strong,
.artifact-sink-main small,
.artifact-sink-owner {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.artifact-sink-main strong {
  color: var(--text);
  font-size: 12px;
}

.artifact-sink-main small {
  color: var(--text-muted);
  font-size: 10px;
}

.provider-mark {
  width: 9px;
  height: 9px;
  flex: 0 0 auto;
  border-radius: 2px;
  background: var(--text-muted);
}

.provider-s3 { background: #16a34a; }
.provider-r2 { background: #f97316; }
.provider-oss { background: #2563eb; }

.artifact-sink-row-actions {
  justify-content: flex-end;
}

.danger-icon {
  color: var(--danger);
}

.artifact-sink-empty {
  padding: 28px 16px;
  color: var(--text-muted);
  font-size: 12px;
  text-align: center;
}

.artifact-sink-modal {
  width: min(780px, 100%);
}

@media (max-width: 900px) {
  .artifact-sink-header {
    display: none;
  }

  .artifact-sink-row {
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 9px 12px;
    padding: 14px;
  }

  .artifact-sink-row > div {
    min-width: 0;
  }

  .artifact-sink-row > div:nth-child(2),
  .artifact-sink-row > div:nth-child(3),
  .artifact-sink-row > div:nth-child(4),
  .artifact-sink-row > div:nth-child(5) {
    grid-column: 1;
  }

  .artifact-sink-row-actions {
    grid-column: 2;
    grid-row: 1 / span 5;
  }
}

@media (max-width: 640px) {
  .artifact-sink-title {
    align-items: flex-start;
  }

  .artifact-sink-actions {
    flex-shrink: 0;
  }

  .artifact-sink-modal .form-grid {
    grid-template-columns: 1fr;
  }

  .artifact-sink-modal .form-span-2 {
    grid-column: span 1;
  }
}
</style>
