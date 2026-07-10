<script setup lang="ts">
import { onMounted } from 'vue'
import { RouterView } from 'vue-router'
import { useAppStore } from '@/stores/app'

const app = useAppStore()

onMounted(() => {
  app.loadPublicSettings()
})
</script>

<template>
  <RouterView />
</template>
