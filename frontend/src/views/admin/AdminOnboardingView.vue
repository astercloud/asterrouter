<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { Edit3, KeyRound, Plus, RefreshCw, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createApplication, getApplications, updateApplication } from '@/api/control'
import type { Application, ApplicationRequest } from '@/types'

const { t, locale } = useI18n()
const applications = ref<Application[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const query = ref('')
const statusFilter = ref('')
const modalOpen = ref(false)
const editing = ref<Application | null>(null)
const nameInput = ref<HTMLInputElement | null>(null)
let modalTrigger: HTMLElement | null = null

const form = reactive<ApplicationRequest>({
  name: '',
  slug: '',
  entitlement_reference: '',
  concurrency_limit: 0,
  status: 'active'
})

const filteredApplications = computed(() => {
  const keyword = query.value.trim().toLocaleLowerCase(locale.value)
  return applications.value.filter((application) => {
    if (statusFilter.value && application.status !== statusFilter.value) return false
    if (!keyword) return true
    return [application.name, application.slug, application.entitlement_reference]
      .some((value) => value.toLocaleLowerCase(locale.value).includes(keyword))
  })
})

function resetForm() {
  Object.assign(form, {
    name: '',
    slug: '',
    entitlement_reference: '',
    concurrency_limit: 0,
    status: 'active'
  })
}

function openCreate(event?: Event) {
  modalTrigger = event?.currentTarget as HTMLElement | null
  editing.value = null
  resetForm()
  modalOpen.value = true
  void focusNameInput()
}

function openEdit(application: Application, event?: Event) {
  modalTrigger = event?.currentTarget as HTMLElement | null
  editing.value = application
  Object.assign(form, {
    name: application.name,
    slug: application.slug,
    entitlement_reference: application.entitlement_reference,
    concurrency_limit: application.concurrency_limit,
    status: application.status
  })
  modalOpen.value = true
  void focusNameInput()
}

async function focusNameInput() {
  await nextTick()
  nameInput.value?.focus()
}

function closeModal() {
  modalOpen.value = false
  editing.value = null
  nextTick(() => modalTrigger?.focus())
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    applications.value = await getApplications()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const payload = { ...form }
    if (editing.value) {
      await updateApplication(editing.value.id, payload)
      message.value = t('applications.updated')
    } else {
      await createApplication(payload)
      message.value = t('applications.created')
    }
    closeModal()
    await load()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}

function statusClass(status: string) {
  return status === 'active' ? 'status-success' : ''
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  }).format(date)
}

function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : t('common.failed')
}

onMounted(load)
</script>

<template>
  <main class="content crud-page application-page">
    <section class="page-header">
      <div>
        <h1>{{ t('applications.title') }}</h1>
        <p>{{ t('applications.subtitle') }}</p>
      </div>
      <div class="page-header-actions">
        <RouterLink class="button secondary" to="/console/applications/credentials">
          <KeyRound :size="17" />{{ t('applications.credentials') }}
        </RouterLink>
        <button class="button" type="button" @click="openCreate">
          <Plus :size="17" />{{ t('applications.new') }}
        </button>
      </div>
    </section>

    <section class="table-toolbar application-toolbar" :aria-label="t('applications.filters')">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('applications.searchPlaceholder')" />
      </label>
      <div class="application-toolbar-actions">
        <span class="application-result-count">{{ t('applications.resultCount', { count: filteredApplications.length }) }}</span>
        <select v-model="statusFilter" :aria-label="t('applications.status')">
          <option value="">{{ t('applications.allStatuses') }}</option>
          <option value="active">{{ t('applications.statuses.active') }}</option>
          <option value="disabled">{{ t('applications.statuses.disabled') }}</option>
        </select>
        <button class="icon-button" type="button" :disabled="loading" :aria-label="t('common.refresh')" :title="t('common.refresh')" @click="load">
          <RefreshCw :class="{ spin: loading }" :size="17" />
        </button>
      </div>
    </section>

    <div v-if="message" class="notice success" role="status">{{ message }}</div>
    <div v-if="error" class="notice" role="alert">{{ error }}</div>

    <section class="panel table-panel content-fit application-list-panel">
      <div class="panel-body table-scroll">
        <table class="data-table application-table">
          <thead>
            <tr>
              <th>{{ t('applications.application') }}</th>
              <th>{{ t('applications.status') }}</th>
              <th>{{ t('applications.concurrency') }}</th>
              <th>{{ t('applications.entitlement') }}</th>
              <th>{{ t('applications.updatedAt') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="application in filteredApplications" :key="application.id">
              <td class="application-cell-primary" :data-label="t('applications.application')">
                <strong>{{ application.name }}</strong>
                <span>{{ application.slug }}</span>
              </td>
              <td :data-label="t('applications.status')">
                <span class="pill" :class="statusClass(application.status)">{{ t(`applications.statuses.${application.status}`) }}</span>
              </td>
              <td :data-label="t('applications.concurrency')">
                {{ application.concurrency_limit > 0 ? application.concurrency_limit : t('applications.unlimited') }}
              </td>
              <td :data-label="t('applications.entitlement')">
                <span class="application-reference">{{ application.entitlement_reference || '-' }}</span>
              </td>
              <td :data-label="t('applications.updatedAt')">{{ formatDate(application.updated_at) }}</td>
              <td class="application-cell-actions" :data-label="t('common.actions')">
                <button class="icon-button" type="button" :aria-label="t('applications.editNamed', { name: application.name })" :title="t('common.edit')" @click="openEdit(application, $event)">
                  <Edit3 :size="16" />
                </button>
              </td>
            </tr>
            <tr v-if="!filteredApplications.length">
              <td colspan="6" class="empty-cell application-empty">
                <strong>{{ loading ? t('common.loading') : t('applications.empty') }}</strong>
                <span v-if="!loading">{{ query || statusFilter ? t('applications.emptyFiltered') : t('applications.emptyHelp') }}</span>
                <button v-if="!loading && !query && !statusFilter" class="button" type="button" @click="openCreate">
                  <Plus :size="17" />{{ t('applications.new') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop" @click.self="closeModal" @keydown.esc="closeModal">
      <section class="modal-card application-modal" role="dialog" aria-modal="true" :aria-label="editing ? t('applications.edit') : t('applications.new')">
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('applications.edit') : t('applications.new') }}</h2>
            <p>{{ t('applications.formSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" :aria-label="t('common.close')" :title="t('common.close')" @click="closeModal">
            <X :size="18" />
          </button>
        </header>
        <form class="application-form" @submit.prevent="save">
          <div class="modal-body">
            <fieldset class="form-fieldset" :disabled="saving">
              <div class="form-grid application-form-grid">
                <div class="field">
                  <label for="application-name">{{ t('applications.name') }}</label>
                  <input id="application-name" ref="nameInput" v-model.trim="form.name" required maxlength="120" autocomplete="off" />
                </div>
                <div class="field">
                  <label for="application-slug">{{ t('applications.slug') }}</label>
                  <input id="application-slug" v-model.trim="form.slug" required pattern="[a-z0-9]+(?:-[a-z0-9]+)*" placeholder="customer-service" autocomplete="off" />
                  <small>{{ t('applications.slugHelp') }}</small>
                </div>
                <div class="field">
                  <label for="application-concurrency">{{ t('applications.concurrency') }}</label>
                  <input id="application-concurrency" v-model.number="form.concurrency_limit" type="number" min="0" />
                  <small>{{ t('applications.concurrencyHelp') }}</small>
                </div>
                <div class="field">
                  <label for="application-status">{{ t('applications.status') }}</label>
                  <select id="application-status" v-model="form.status">
                    <option value="active">{{ t('applications.statuses.active') }}</option>
                    <option value="disabled">{{ t('applications.statuses.disabled') }}</option>
                  </select>
                </div>
                <div class="field application-form-wide">
                  <label for="application-entitlement">{{ t('applications.entitlement') }}</label>
                  <input id="application-entitlement" v-model.trim="form.entitlement_reference" :placeholder="t('applications.entitlementPlaceholder')" autocomplete="off" />
                  <small>{{ t('applications.entitlementHelp') }}</small>
                </div>
              </div>
            </fieldset>
          </div>
          <footer class="modal-footer">
            <button class="button secondary" type="button" :disabled="saving" @click="closeModal">{{ t('common.cancel') }}</button>
            <button class="button" type="submit" :disabled="saving">{{ saving ? t('common.saving') : t('common.save') }}</button>
          </footer>
        </form>
      </section>
    </div>
  </main>
</template>
