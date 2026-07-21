<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ArrowLeft, RefreshCw } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { getPluginFrontendAsset, getPluginFrontendContribution } from '@/api/plugins'
import { isNotFoundError } from '@/api/client'
import type { PluginFrontendContribution, PluginFrontendContributionSurface } from '@/types'

const route = useRoute()
const router = useRouter()
const loading = ref(true)
const error = ref('')
const loaded = ref(false)
const contribution = ref<PluginFrontendContribution | null>(null)
const mountRoot = ref<HTMLDivElement | null>(null)
let pluginStyle: HTMLStyleElement | null = null
let pluginScript: HTMLScriptElement | null = null

const pluginID = computed(() => String(route.params.pluginId || '').trim())
const surfaceRoot = computed(() => {
  const path = route.path
  if (path.startsWith('/console/')) return '/console'
  if (path.startsWith('/operator/')) return '/operator'
  if (path.startsWith('/platform/')) return '/platform'
  return '/admin'
})
const contributionSurfaceName = computed(() => {
  if (surfaceRoot.value === '/console') return 'console.plugins'
  if (surfaceRoot.value === '/operator') return 'operator.plugins'
  if (surfaceRoot.value === '/platform') return 'platform.plugins'
  return 'admin.plugins'
})
const contributionSurface = computed<PluginFrontendContributionSurface | null>(() => {
  const surfaces = contribution.value?.surfaces || []
  return surfaces.find((item) => item.surface === contributionSurfaceName.value) || surfaces.find((item) => item.surface === 'admin.plugins') || surfaces[0] || null
})

function backToCenter() {
  void router.push(`${surfaceRoot.value}/plugins`)
}

function assetURL(id: string, assetPath: string) {
  const path = assetPath.split('/').filter(Boolean).map((segment) => encodeURIComponent(segment)).join('/')
  return `/api/v1/admin/plugins/${encodeURIComponent(id)}/frontend/assets/${path}`
}

function mimeType(assetPath: string) {
  const extension = assetPath.split('.').pop()?.toLowerCase()
  if (extension === 'png') return 'image/png'
  if (extension === 'jpg' || extension === 'jpeg') return 'image/jpeg'
  if (extension === 'webp') return 'image/webp'
  if (extension === 'gif') return 'image/gif'
  if (extension === 'svg') return 'image/svg+xml'
  return 'application/octet-stream'
}

function dataURL(buffer: ArrayBuffer, assetPath: string) {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (let index = 0; index < bytes.length; index += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(index, index + 0x8000))
  }
  return `data:${mimeType(assetPath)};base64,${btoa(binary)}`
}

async function inlineAssetReferences(source: string, id: string) {
  const matches = source.match(/\/?assets\/[A-Za-z0-9._/-]+/g) || []
  const assetPaths = Array.from(new Set(matches)).filter((path) => !path.endsWith('/index.js'))
  let output = source
  await Promise.all(assetPaths.map(async (reference) => {
    const assetPath = reference.replace(/^\/+/, '')
    try {
      const raw = await getPluginFrontendAsset(id, assetPath, 'arraybuffer')
      const replacement = dataURL(raw as ArrayBuffer, assetPath)
      output = output.split(reference).join(replacement)
    } catch (cause) {
      if (!isNotFoundError(cause)) throw cause
    }
  }))
  return output
}

function scopePluginCSS(source: string) {
  try {
    const styleSheet = new CSSStyleSheet()
    styleSheet.replaceSync(source)
    scopeCSSRules(styleSheet.cssRules)
    return Array.from(styleSheet.cssRules, (rule) => rule.cssText).join('\n')
  } catch {
    throw new Error('当前浏览器不支持插件样式隔离')
  }
}

function scopeCSSRules(rules: CSSRuleList) {
  for (const rule of Array.from(rules)) {
    if (rule instanceof CSSStyleRule) {
      rule.selectorText = rule.selectorText
        .split(',')
        .map((selector) => {
          const value = selector.trim()
          if (value === ':root' || value === 'body') return '#aster-plugin-root'
          if (value.startsWith('body ')) return `#aster-plugin-root ${value.slice(5)}`
          if (value.startsWith('#aster-plugin-root')) return value
          return `#aster-plugin-root ${value}`
        })
        .join(', ')
      continue
    }
    if ('cssRules' in rule) scopeCSSRules((rule as CSSGroupingRule).cssRules)
  }
}

function clearPlugin() {
  pluginStyle?.remove()
  pluginStyle = null
  pluginScript?.remove()
  pluginScript = null
  if (mountRoot.value) mountRoot.value.innerHTML = ''
  loaded.value = false
}

async function loadContribution(id: string) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      return await getPluginFrontendContribution(id)
    } catch (cause) {
      if (!isNotFoundError(cause) || attempt === 2) throw cause
      await new Promise((resolve) => window.setTimeout(resolve, 300 * (attempt + 1)))
    }
  }
  throw new Error('插件贡献面暂时不可用')
}

async function loadPlugin() {
  clearPlugin()
  loading.value = true
  error.value = ''
  try {
    if (!pluginID.value) throw new Error('插件标识缺失')
    contribution.value = await loadContribution(pluginID.value)
    const surface = contributionSurface.value
    if (!surface?.asset) throw new Error('插件未提供可加载的工作台入口')
    if (!mountRoot.value) throw new Error('插件挂载容器不可用')
    mountRoot.value.className = 'plugin-host-root'

    if (surface.style) {
      const css = await getPluginFrontendAsset(pluginID.value, surface.style, 'text')
      pluginStyle = document.createElement('style')
      pluginStyle.dataset.pluginFrontend = pluginID.value
      pluginStyle.textContent = scopePluginCSS(String(css))
      document.head.appendChild(pluginStyle)
    }

    const source = await getPluginFrontendAsset(pluginID.value, surface.asset, 'text')
    const script = document.createElement('script')
    script.type = 'text/javascript'
    script.dataset.pluginFrontend = pluginID.value
    script.textContent = await inlineAssetReferences(String(source), pluginID.value)
    pluginScript = script
    document.body.appendChild(script)
    loaded.value = true
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '插件工作台加载失败'
    clearPlugin()
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadPlugin()
})

onBeforeUnmount(clearPlugin)
</script>

<template>
  <main class="content plugin-frontend-page">
    <header class="plugin-frontend-toolbar">
      <div class="plugin-frontend-heading">
        <button class="button secondary" type="button" aria-label="返回插件中心" @click="backToCenter">
          <ArrowLeft :size="16" />
          插件中心
        </button>
        <div>
          <span class="eyebrow-label">PLUGIN WORKBENCH</span>
          <h1>{{ contributionSurface?.title || '插件工作台' }}</h1>
          <p>已安装插件通过 AsterRouter 宿主加载，生成请求仍由宿主统一处理。</p>
        </div>
      </div>
      <button class="button secondary" type="button" :disabled="loading" aria-label="重新加载插件工作台" @click="loadPlugin">
        <RefreshCw :size="16" />
        {{ loading ? '加载中' : '重新加载' }}
      </button>
    </header>

    <div v-if="error" class="notice plugin-frontend-error" role="alert">
      {{ error }}
      <button class="button secondary tiny-button" type="button" @click="loadPlugin">重试</button>
    </div>
    <div v-if="loading" class="plugin-frontend-loading" role="status">正在加载已安装插件工作台…</div>
    <div id="aster-plugin-root" ref="mountRoot" class="plugin-frontend-mount" :class="{ 'is-hidden': !loaded && !loading }" />
  </main>
</template>
