<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { BadgeDollarSign, Edit3, Plus, RefreshCw, Save, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createModelPricing, getModelPricings, updateModelPricing } from '@/api/control'
import type { ModelPricing, ModelPricingRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const pricings = ref<ModelPricing[]>([])
const query = ref('')
const statusFilter = ref('')
const modalOpen = ref(false)
const editing = ref<ModelPricing | null>(null)

const form = reactive<ModelPricingRequest>({
  model: '',
  currency: 'USD',
  input_price_cents_per_1m_tokens: 0,
  output_price_cents_per_1m_tokens: 0,
  status: 'active'
})

const filteredPricings = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return pricings.value.filter((pricing) => {
    if (statusFilter.value && pricing.status !== statusFilter.value) return false
    if (!keyword) return true
    return [pricing.model, pricing.currency, pricing.status].some((value) => value.toLowerCase().includes(keyword))
  })
})

const summary = computed(() => ({
  total: pricings.value.length,
  active: pricings.value.filter((item) => item.status === 'active').length,
  disabled: pricings.value.filter((item) => item.status === 'disabled').length,
  priced: pricings.value.filter((item) => item.input_price_cents_per_1m_tokens > 0 || item.output_price_cents_per_1m_tokens > 0).length
}))

function resetForm() {
  Object.assign(form, {
    model: '',
    currency: 'USD',
    input_price_cents_per_1m_tokens: 0,
    output_price_cents_per_1m_tokens: 0,
    status: 'active'
  })
}

function openCreate() {
  editing.value = null
  resetForm()
  modalOpen.value = true
}

function openEdit(pricing: ModelPricing) {
  editing.value = pricing
  Object.assign(form, {
    model: pricing.model,
    currency: pricing.currency,
    input_price_cents_per_1m_tokens: pricing.input_price_cents_per_1m_tokens,
    output_price_cents_per_1m_tokens: pricing.output_price_cents_per_1m_tokens,
    status: pricing.status
  })
  modalOpen.value = true
}

function closeModal() {
  modalOpen.value = false
  editing.value = null
}

function formatMoney(cents: number, currency: string): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: currency || 'USD',
    minimumFractionDigits: 2
  }).format(cents / 100)
}

function statusClass(status: string): string {
  if (status === 'active') return 'status-success'
  if (status === 'disabled') return 'status-danger'
  return 'status-warning'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    pricings.value = await getModelPricings()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const payload = { ...form, currency: form.currency.trim().toUpperCase() || 'USD' }
    if (editing.value) {
      await updateModelPricing(editing.value.id, payload)
      message.value = t('modelPricings.updated')
    } else {
      await createModelPricing(payload)
      message.value = t('modelPricings.created')
    }
    closeModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.modelPricings') }}</h1>
        <p>{{ t('modelPricings.subtitle') }}</p>
      </div>
      <button class="button" type="button" @click="openCreate">
        <Plus :size="17" />
        {{ t('modelPricings.newPricing') }}
      </button>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.total }}</strong>{{ t('modelPricings.models') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.disabled }}</strong>{{ t('providers.disabled') }}</span>
      <span><strong>{{ summary.priced }}</strong>{{ t('modelPricings.priced') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('modelPricings.searchPlaceholder')" />
      </label>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">active</option>
        <option value="disabled">disabled</option>
      </select>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('modelPricings.model') }}</th>
              <th>{{ t('modelPricings.currency') }}</th>
              <th>{{ t('modelPricings.inputPrice') }}</th>
              <th>{{ t('modelPricings.outputPrice') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="pricing in filteredPricings" :key="pricing.id">
              <td>
                <strong>{{ pricing.model }}</strong>
                <span>{{ pricing.id }}</span>
              </td>
              <td>{{ pricing.currency }}</td>
              <td>
                <strong>{{ formatMoney(pricing.input_price_cents_per_1m_tokens, pricing.currency) }}</strong>
                <span>{{ t('modelPricings.perMillionInput') }}</span>
              </td>
              <td>
                <strong>{{ formatMoney(pricing.output_price_cents_per_1m_tokens, pricing.currency) }}</strong>
                <span>{{ t('modelPricings.perMillionOutput') }}</span>
              </td>
              <td><span class="pill" :class="statusClass(pricing.status)">{{ pricing.status }}</span></td>
              <td>
                <button class="button secondary" type="button" @click="openEdit(pricing)">
                  <Edit3 :size="15" />
                  {{ t('common.edit') }}
                </button>
              </td>
            </tr>
            <tr v-if="!filteredPricings.length">
              <td colspan="6" class="empty-cell">{{ t('modelPricings.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modalOpen" class="modal-backdrop">
      <form class="modal-card" @submit.prevent="save">
        <header class="modal-header">
          <div>
            <h2>{{ editing ? t('modelPricings.editPricing') : t('modelPricings.newPricing') }}</h2>
            <p>{{ t('modelPricings.modalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeModal">
            <X :size="18" />
          </button>
        </header>
        <div class="modal-body form-grid">
          <div class="field">
            <label>{{ t('modelPricings.model') }}</label>
            <input v-model="form.model" required placeholder="gpt-4o-mini" />
          </div>
          <div class="field">
            <label>{{ t('modelPricings.currency') }}</label>
            <input v-model="form.currency" required maxlength="3" />
          </div>
          <div class="field">
            <label>{{ t('modelPricings.inputPrice') }}</label>
            <input v-model.number="form.input_price_cents_per_1m_tokens" type="number" min="0" required />
            <span class="hint">{{ t('modelPricings.inputHelp') }}</span>
          </div>
          <div class="field">
            <label>{{ t('modelPricings.outputPrice') }}</label>
            <input v-model.number="form.output_price_cents_per_1m_tokens" type="number" min="0" required />
            <span class="hint">{{ t('modelPricings.outputHelp') }}</span>
          </div>
          <div class="field">
            <label>{{ t('providers.status') }}</label>
            <select v-model="form.status">
              <option value="active">active</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
          <button class="button" type="submit" :disabled="saving">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </form>
    </div>
  </main>
</template>
