<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { RotateCw, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(defineProps<{
  open: boolean
  keyName: string
  saving?: boolean
}>(), {
  saving: false
})

const emit = defineEmits<{
  cancel: []
  confirm: [gracePeriodSeconds: number]
}>()

const { t } = useI18n()
const gracePeriodSeconds = ref(0)
const graceSelect = ref<HTMLSelectElement | null>(null)
const overlapHelp = computed(() => t(gracePeriodSeconds.value === 0 ? 'apiKeys.rotationImmediateHelp' : 'apiKeys.rotationGraceHelp'))

watch(() => props.open, async (open) => {
  if (!open) return
  gracePeriodSeconds.value = 0
  await nextTick()
  graceSelect.value?.focus()
}, { immediate: true })
</script>

<template>
  <div v-if="open" class="modal-backdrop" @click.self="emit('cancel')">
    <form class="modal-card" role="dialog" aria-modal="true" aria-labelledby="api-key-rotation-title" @submit.prevent="emit('confirm', gracePeriodSeconds)" @keydown.esc.prevent="emit('cancel')">
      <header class="modal-header">
        <div>
          <h2 id="api-key-rotation-title">{{ t('apiKeys.rotationTitle') }}</h2>
          <p>{{ t('apiKeys.rotationSubtitle', { name: keyName }) }}</p>
        </div>
        <button class="icon-button" type="button" :title="t('common.close')" @click="emit('cancel')"><X :size="19" /></button>
      </header>
      <div class="modal-body">
        <div class="field">
          <label for="api-key-rotation-grace">{{ t('apiKeys.rotationGrace') }}</label>
          <select id="api-key-rotation-grace" ref="graceSelect" v-model.number="gracePeriodSeconds" :disabled="saving">
            <option :value="0">{{ t('apiKeys.rotationImmediate') }}</option>
            <option :value="300">{{ t('apiKeys.rotationFiveMinutes') }}</option>
            <option :value="3600">{{ t('apiKeys.rotationOneHour') }}</option>
            <option :value="86400">{{ t('apiKeys.rotationOneDay') }}</option>
          </select>
          <small>{{ overlapHelp }}</small>
        </div>
      </div>
      <footer class="modal-footer">
        <button class="button secondary" type="button" :disabled="saving" @click="emit('cancel')">{{ t('common.cancel') }}</button>
        <button class="button" type="submit" :disabled="saving"><RotateCw :size="17" />{{ saving ? t('common.saving') : t('apiKeys.rotationConfirm') }}</button>
      </footer>
    </form>
  </div>
</template>
