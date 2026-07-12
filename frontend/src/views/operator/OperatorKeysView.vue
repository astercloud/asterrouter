<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { KeyRound, RefreshCw, RotateCw, Search, ShieldOff } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { disableOperatorCustomerKey, getOperatorCustomerKeys, listOperatorResource, rotateOperatorCustomerKey } from '@/api/operator'
import type { APIKeyRecord, OperatorCustomer } from '@/types'

const { t } = useI18n()
const keys = ref<APIKeyRecord[]>([])
const customers = ref<OperatorCustomer[]>([])
const query = ref('')
const error = ref('')
const createdKey = ref('')

const customerName = (id: string) => customers.value.find((item) => item.id === id)?.name || id
const filtered = () => keys.value.filter((item) => !query.value || `${item.name} ${item.fingerprint} ${customerName(item.customer_id)}`.toLowerCase().includes(query.value.toLowerCase()))

async function load() {
  try {
    ;[keys.value, customers.value] = await Promise.all([getOperatorCustomerKeys(), listOperatorResource('customers') as Promise<OperatorCustomer[]>])
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

async function rotate(item: APIKeyRecord) {
  try {
    const response = await rotateOperatorCustomerKey(item.id)
    createdKey.value = response.key
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

async function disable(item: APIKeyRecord) {
  if (!window.confirm(t('operatorDomain.disableKeyConfirm'))) return
  try {
    await disableOperatorCustomerKey(item.id)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header"><div><h1>{{ t('operatorDomain.keyList') }}</h1><p>{{ t('operatorDomain.keySummary') }}</p></div></section>
    <section class="table-toolbar"><label class="search-box"><Search :size="17"/><input v-model="query" :placeholder="t('operatorCrud.search')"/></label><button class="button secondary" @click="load"><RefreshCw :size="17"/>{{ t('common.refresh') }}</button></section>
    <div v-if="error" class="notice">{{ error }}</div>
    <div v-if="createdKey" class="notice success"><KeyRound :size="16"/><code>{{ createdKey }}</code></div>
    <section class="panel table-panel"><div class="panel-body table-scroll"><table class="data-table crud-table"><thead><tr><th>{{ t('apiKeys.name') }}</th><th>{{ t('operatorDomain.customer') }}</th><th>{{ t('apiKeys.models') }}</th><th>{{ t('providers.status') }}</th><th>{{ t('common.actions') }}</th></tr></thead><tbody><tr v-for="item in filtered()" :key="item.id"><td><strong>{{ item.name }}</strong><span>{{ item.fingerprint }}</span></td><td>{{ customerName(item.customer_id) }}</td><td>{{ item.model_allowlist.join(', ') }}</td><td><span class="pill" :class="item.status === 'active' ? 'status-success' : 'status-warning'">{{ item.status }}</span></td><td class="table-actions"><button class="icon-button" :title="t('operatorDomain.rotateKey')" @click="rotate(item)"><RotateCw :size="16"/></button><button v-if="item.status === 'active'" class="icon-button" :title="t('operatorDomain.disableKey')" @click="disable(item)"><ShieldOff :size="16"/></button></td></tr><tr v-if="!filtered().length"><td colspan="5" class="empty-cell"></td></tr></tbody></table></div></section>
  </main>
</template>
