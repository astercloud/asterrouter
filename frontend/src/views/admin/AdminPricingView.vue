<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PricingRulesView from '@/components/pricing/PricingRulesView.vue'

const { t } = useI18n()
</script>

<template>
  <PricingRulesView :title="t('pricingRules.adminTitle')" :subtitle="t('pricingRules.adminSubtitle')" />
</template>
