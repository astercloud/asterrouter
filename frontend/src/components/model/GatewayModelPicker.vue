<script setup lang="ts">
import { computed } from 'vue'
import { AlertTriangle, Check } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import type { GatewayModel } from '@/types'

const props = withDefaults(defineProps<{
  models: GatewayModel[]
  modelValue: string[]
  disabled?: boolean
  ariaLabel?: string
}>(), {
  disabled: false,
  ariaLabel: ''
})

const emit = defineEmits<{
  'update:modelValue': [models: string[]]
}>()

const { t } = useI18n()

const activeModels = computed(() => {
  const seen = new Set<string>()
  return props.models.filter((model) => {
    const modelID = model.model_id.trim()
    if (model.status !== 'active' || !modelID || seen.has(modelID)) return false
    seen.add(modelID)
    return true
  })
})

const activeModelIDs = computed(() => new Set(activeModels.value.map((model) => model.model_id)))
const historicalModels = computed(() => props.modelValue.filter((model) => model && !activeModelIDs.value.has(model)))

function toggle(model: string) {
  if (props.disabled) return
  const next = props.modelValue.includes(model)
    ? props.modelValue.filter((item) => item !== model)
    : [...props.modelValue, model]
  emit('update:modelValue', Array.from(new Set(next.filter(Boolean))))
}
</script>

<template>
  <div class="gateway-model-picker" role="group" :aria-label="ariaLabel || t('apiKeys.models')">
    <div v-if="activeModels.length" class="chip-list gateway-model-options">
      <button
        v-for="model in activeModels"
        :key="model.id"
        class="pill gateway-model-option"
        :class="{ 'status-success': modelValue.includes(model.model_id) }"
        type="button"
        :disabled="disabled"
        :aria-pressed="modelValue.includes(model.model_id)"
        data-model-state="active"
        @click="toggle(model.model_id)"
      >
        <Check v-if="modelValue.includes(model.model_id)" :size="13" />
        <span>{{ model.model_id }}</span>
      </button>
    </div>
    <p v-else class="gateway-model-empty">{{ t('apiKeys.noActiveModels') }}</p>

    <div v-if="historicalModels.length" class="gateway-model-history">
      <span class="hint">{{ t('apiKeys.historicalModels') }}</span>
      <div class="chip-list">
        <button
          v-for="model in historicalModels"
          :key="model"
          class="pill status-warning gateway-model-option"
          type="button"
          :disabled="disabled"
          :title="t('apiKeys.historicalModelHint', { model })"
          data-model-state="historical"
          @click="toggle(model)"
        >
          <AlertTriangle :size="13" />
          <span>{{ model }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.gateway-model-picker,
.gateway-model-history {
  display: grid;
  gap: 8px;
  min-width: 0;
}

.gateway-model-option {
  min-height: 32px;
  max-width: 100%;
  cursor: pointer;
}

.gateway-model-option span {
  overflow-wrap: anywhere;
}

.gateway-model-option:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.gateway-model-empty {
  margin: 0;
  color: var(--text-muted);
}
</style>
