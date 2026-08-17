<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watchEffect } from 'vue'
import {
  Activity,
  ArrowRight,
  Blocks,
  Check,
  CircleDollarSign,
  FileKey2,
  Gauge,
  GitFork,
  KeyRound,
  Languages,
  ListOrdered,
  Menu,
  Network,
  Route,
  ShieldCheck,
  SquareActivity,
  X
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import MarketingHeroVisual from '@/components/marketing/MarketingHeroVisual.vue'
import { availableLocales, getLocale, setLocale, type LocaleCode } from '@/i18n'
import { entryForUser } from '@/router/access'
import type { AuthUser } from '@/types'

const { t } = useI18n()
const menuOpen = ref(false)
const scrolled = ref(false)

const entryPath = computed(() => {
  if (!localStorage.getItem('asterrouter_admin_token')) return '/login'
  try {
    const user = JSON.parse(localStorage.getItem('asterrouter_admin_user') || 'null') as AuthUser | null
    return entryForUser(user)
  } catch {
    return '/login'
  }
})

const decisionStages = computed(() => [
  { icon: KeyRound, title: t('marketing.decision.stages.identity'), value: t('marketing.decision.stageValues.identity') },
  { icon: ShieldCheck, title: t('marketing.decision.stages.access'), value: t('marketing.decision.stageValues.access') },
  { icon: Route, title: t('marketing.decision.stages.routing'), value: t('marketing.decision.stageValues.routing') },
  { icon: Gauge, title: t('marketing.decision.stages.scheduling'), value: t('marketing.decision.stageValues.scheduling') },
  { icon: SquareActivity, title: t('marketing.decision.stages.evidence'), value: t('marketing.decision.stageValues.evidence') }
])

const capabilityRows = computed(() => [
  { icon: Blocks, title: t('marketing.capabilities.applications.title'), scope: t('marketing.capabilities.applications.scope'), evidence: t('marketing.capabilities.applications.evidence') },
  { icon: Network, title: t('marketing.capabilities.supply.title'), scope: t('marketing.capabilities.supply.scope'), evidence: t('marketing.capabilities.supply.evidence') },
  { icon: ShieldCheck, title: t('marketing.capabilities.policies.title'), scope: t('marketing.capabilities.policies.scope'), evidence: t('marketing.capabilities.policies.evidence') },
  { icon: CircleDollarSign, title: t('marketing.capabilities.cost.title'), scope: t('marketing.capabilities.cost.scope'), evidence: t('marketing.capabilities.cost.evidence') },
  { icon: FileKey2, title: t('marketing.capabilities.governance.title'), scope: t('marketing.capabilities.governance.scope'), evidence: t('marketing.capabilities.governance.evidence') }
])

const routingPreferences = computed(() => [
  { id: '01', name: t('marketing.routing.preferences.cost'), detail: t('marketing.routing.preferenceDetails.cost') },
  { id: '02', name: t('marketing.routing.preferences.speed'), detail: t('marketing.routing.preferenceDetails.speed') },
  { id: '03', name: t('marketing.routing.preferences.stability'), detail: t('marketing.routing.preferenceDetails.stability') },
  { id: '04', name: t('marketing.routing.preferences.balanced'), detail: t('marketing.routing.preferenceDetails.balanced') }
])

function changeLocale(event: Event) {
  setLocale((event.target as HTMLSelectElement).value as LocaleCode)
  closeMenu()
}

function closeMenu() {
  menuOpen.value = false
}

function handleScroll() {
  scrolled.value = window.scrollY > 20
}

watchEffect(() => {
  document.title = t('marketing.metaTitle')
  let meta = document.querySelector<HTMLMetaElement>('meta[name="description"]')
  if (!meta) {
    meta = document.createElement('meta')
    meta.name = 'description'
    document.head.appendChild(meta)
  }
  meta.content = t('marketing.metaDescription')
})

onMounted(() => {
  handleScroll()
  window.addEventListener('scroll', handleScroll, { passive: true })
})

onBeforeUnmount(() => window.removeEventListener('scroll', handleScroll))
</script>

<template>
  <div class="marketing-page">
    <header class="marketing-header" :class="{ scrolled, open: menuOpen }">
      <div class="marketing-shell header-inner">
        <a class="marketing-brand" href="#overview" aria-label="AsterRouter" @click="closeMenu">
          <span class="marketing-brand-mark" aria-hidden="true">AR</span>
          <span><strong>AsterRouter</strong><small>{{ t('marketing.brandTagline') }}</small></span>
        </a>
        <nav class="desktop-nav" :aria-label="t('marketing.navLabel')">
          <a href="#product">{{ t('marketing.nav.product') }}</a>
          <a href="#routing">{{ t('marketing.nav.routing') }}</a>
          <a href="#governance">{{ t('marketing.nav.governance') }}</a>
          <a href="https://github.com/astercloud/asterrouter" target="_blank" rel="noreferrer">{{ t('marketing.nav.repository') }}</a>
        </nav>
        <div class="header-actions">
          <label class="marketing-locale">
            <Languages :size="15" aria-hidden="true" />
            <select :value="getLocale()" :aria-label="t('nav.language')" @change="changeLocale">
              <option v-for="item in availableLocales" :key="item.code" :value="item.code">{{ item.label }}</option>
            </select>
          </label>
          <RouterLink class="header-entry" :to="entryPath">{{ t('marketing.enterConsole') }}<ArrowRight :size="15" /></RouterLink>
        </div>
        <button
          class="mobile-menu-button"
          type="button"
          :aria-label="menuOpen ? t('marketing.closeNav') : t('marketing.openNav')"
          :aria-expanded="menuOpen"
          @click="menuOpen = !menuOpen"
        >
          <X v-if="menuOpen" :size="21" />
          <Menu v-else :size="21" />
        </button>
      </div>
      <nav v-if="menuOpen" class="mobile-nav" :aria-label="t('marketing.mobileNavLabel')">
        <a href="#product" @click="closeMenu">{{ t('marketing.nav.product') }}</a>
        <a href="#routing" @click="closeMenu">{{ t('marketing.nav.routing') }}</a>
        <a href="#governance" @click="closeMenu">{{ t('marketing.nav.governance') }}</a>
        <a href="https://github.com/astercloud/asterrouter" target="_blank" rel="noreferrer" @click="closeMenu">{{ t('marketing.nav.repository') }}</a>
        <label class="mobile-locale">
          <Languages :size="16" aria-hidden="true" />
          <select :value="getLocale()" :aria-label="t('nav.language')" @change="changeLocale">
            <option v-for="item in availableLocales" :key="item.code" :value="item.code">{{ item.label }}</option>
          </select>
        </label>
        <RouterLink :to="entryPath" @click="closeMenu">{{ t('marketing.enterConsole') }}<ArrowRight :size="16" /></RouterLink>
      </nav>
    </header>

    <main>
      <section id="overview" class="marketing-hero">
        <MarketingHeroVisual />
        <div class="marketing-shell hero-content">
          <span class="hero-overline"><Activity :size="14" />{{ t('marketing.hero.overline') }}</span>
          <h1>AsterRouter</h1>
          <strong class="hero-category">{{ t('marketing.hero.category') }}</strong>
          <p>{{ t('marketing.hero.description') }}</p>
          <div class="hero-actions">
            <RouterLink class="primary-action" :to="entryPath"><ShieldCheck :size="18" />{{ t('marketing.hero.primaryAction') }}</RouterLink>
            <a class="secondary-action" href="#product"><Route :size="18" />{{ t('marketing.hero.secondaryAction') }}</a>
          </div>
          <div class="hero-facts" :aria-label="t('marketing.hero.factsLabel')">
            <span><Check :size="15" />{{ t('marketing.hero.factProtocols') }}</span>
            <span><Check :size="15" />{{ t('marketing.hero.factEvidence') }}</span>
          </div>
        </div>
      </section>

      <section id="product" class="marketing-section decision-section">
        <div class="marketing-shell">
          <header class="section-heading">
            <span>{{ t('marketing.decision.overline') }}</span>
            <h2>{{ t('marketing.decision.title') }}</h2>
            <p>{{ t('marketing.decision.description') }}</p>
          </header>
          <div class="decision-workbench" :aria-label="t('marketing.decision.workbenchLabel')">
            <aside class="decision-stage-list">
              <div v-for="(stage, index) in decisionStages" :key="stage.title" :class="{ current: index === 2 }">
                <span>{{ String(index + 1).padStart(2, '0') }}</span>
                <component :is="stage.icon" :size="18" />
                <p><strong>{{ stage.title }}</strong><small>{{ stage.value }}</small></p>
              </div>
            </aside>
            <section class="decision-trace">
              <header>
                <div><small>{{ t('marketing.decision.requestLabel') }}</small><strong>req_7f2c8a19</strong></div>
                <span><i></i>{{ t('marketing.decision.policyApplied') }}</span>
              </header>
              <div class="request-context">
                <div><span>{{ t('marketing.decision.application') }}</span><strong>Customer Support Copilot</strong></div>
                <div><span>{{ t('marketing.decision.model') }}</span><strong>enterprise-chat</strong></div>
                <div><span>{{ t('marketing.decision.routeGroup') }}</span><strong>production</strong></div>
              </div>
              <div class="candidate-heading"><span>{{ t('marketing.decision.candidates') }}</span><small>{{ t('marketing.decision.hardRulesFirst') }}</small></div>
              <div class="candidate-row selected">
                <span>01</span><p><strong>Primary East</strong><small>{{ t('marketing.decision.candidatePrimary') }}</small></p><b>{{ t('marketing.decision.selected') }}</b>
              </div>
              <div class="candidate-row">
                <span>02</span><p><strong>Primary West</strong><small>{{ t('marketing.decision.candidateSecondary') }}</small></p><b>{{ t('marketing.decision.standby') }}</b>
              </div>
              <div class="candidate-row excluded">
                <span>03</span><p><strong>Overflow Pool</strong><small>{{ t('marketing.decision.candidateExcluded') }}</small></p><b>{{ t('marketing.decision.excluded') }}</b>
              </div>
              <footer><SquareActivity :size="17" /><span><strong>{{ t('marketing.decision.traceReady') }}</strong><small>{{ t('marketing.decision.traceDetail') }}</small></span><ArrowRight :size="16" /></footer>
            </section>
          </div>
        </div>
      </section>

      <section class="marketing-section capability-section">
        <div class="marketing-shell">
          <header class="section-heading compact">
            <span>{{ t('marketing.capabilities.overline') }}</span>
            <h2>{{ t('marketing.capabilities.title') }}</h2>
            <p>{{ t('marketing.capabilities.description') }}</p>
          </header>
          <div class="capability-table" role="table" :aria-label="t('marketing.capabilities.tableLabel')">
            <div class="capability-head" role="row">
              <span role="columnheader">{{ t('marketing.capabilities.domain') }}</span>
              <span role="columnheader">{{ t('marketing.capabilities.scope') }}</span>
              <span role="columnheader">{{ t('marketing.capabilities.evidence') }}</span>
            </div>
            <article v-for="(row, index) in capabilityRows" :key="row.title" role="row">
              <span class="capability-index">{{ String(index + 1).padStart(2, '0') }}</span>
              <div class="capability-name" role="cell"><component :is="row.icon" :size="19" /><strong>{{ row.title }}</strong></div>
              <p role="cell">{{ row.scope }}</p>
              <p role="cell">{{ row.evidence }}</p>
            </article>
          </div>
        </div>
      </section>

      <section id="routing" class="marketing-section routing-section">
        <div class="marketing-shell routing-layout">
          <header class="section-heading">
            <span>{{ t('marketing.routing.overline') }}</span>
            <h2>{{ t('marketing.routing.title') }}</h2>
            <p>{{ t('marketing.routing.description') }}</p>
            <RouterLink class="inline-link" :to="entryPath">{{ t('marketing.routing.action') }}<ArrowRight :size="15" /></RouterLink>
          </header>
          <div class="routing-contract">
            <div class="routing-contract-head"><span>{{ t('marketing.routing.contract') }}</span><strong>{{ t('marketing.routing.hardBeforeSoft') }}</strong></div>
            <div v-for="preference in routingPreferences" :key="preference.id" class="preference-row">
              <span>{{ preference.id }}</span><p><strong>{{ preference.name }}</strong><small>{{ preference.detail }}</small></p>
            </div>
            <footer><ListOrdered :size="17" /><span><strong>{{ t('marketing.routing.orderedBatches') }}</strong><small>{{ t('marketing.routing.orderedBatchesDetail') }}</small></span></footer>
          </div>
        </div>
      </section>

      <section id="governance" class="marketing-section governance-section">
        <div class="marketing-shell">
          <header class="section-heading governance-heading">
            <span>{{ t('marketing.governance.overline') }}</span>
            <h2>{{ t('marketing.governance.title') }}</h2>
            <p>{{ t('marketing.governance.description') }}</p>
          </header>
          <div class="governance-map">
            <div><small>01</small><strong>{{ t('marketing.governance.callers') }}</strong><span>{{ t('marketing.governance.callersDetail') }}</span></div>
            <ArrowRight :size="22" />
            <div class="router-core"><span class="marketing-brand-mark" aria-hidden="true">AR</span><strong>AsterRouter</strong><small>{{ t('marketing.governance.coreDetail') }}</small></div>
            <ArrowRight :size="22" />
            <div><small>03</small><strong>{{ t('marketing.governance.providers') }}</strong><span>{{ t('marketing.governance.providersDetail') }}</span></div>
          </div>
          <div class="governance-facts">
            <span><Check :size="15" />{{ t('marketing.governance.privateDeployment') }}</span>
            <span><Check :size="15" />{{ t('marketing.governance.enterpriseIdentity') }}</span>
            <span><Check :size="15" />{{ t('marketing.governance.noResale') }}</span>
          </div>
        </div>
      </section>

      <section class="marketing-cta">
        <div class="marketing-shell">
          <span>ASTERROUTER</span>
          <h2>{{ t('marketing.cta.title') }}</h2>
          <p>{{ t('marketing.cta.description') }}</p>
          <div>
            <RouterLink class="primary-action" :to="entryPath"><ShieldCheck :size="18" />{{ t('marketing.cta.primary') }}</RouterLink>
            <a class="secondary-action" href="https://github.com/astercloud/asterrouter" target="_blank" rel="noreferrer"><GitFork :size="18" />{{ t('marketing.cta.repository') }}</a>
          </div>
        </div>
      </section>
    </main>

    <footer class="marketing-footer">
      <div class="marketing-shell footer-main">
        <div><a class="marketing-brand footer-brand" href="#overview"><span class="marketing-brand-mark" aria-hidden="true">AR</span><span><strong>AsterRouter</strong><small>{{ t('marketing.brandTagline') }}</small></span></a><p>{{ t('marketing.footer.description') }}</p></div>
        <nav :aria-label="t('marketing.footer.productLabel')"><strong>{{ t('marketing.footer.productLabel') }}</strong><a href="#product">{{ t('marketing.nav.product') }}</a><a href="#routing">{{ t('marketing.nav.routing') }}</a><a href="#governance">{{ t('marketing.nav.governance') }}</a></nav>
        <nav :aria-label="t('marketing.footer.resourceLabel')"><strong>{{ t('marketing.footer.resourceLabel') }}</strong><a href="https://github.com/astercloud/asterrouter" target="_blank" rel="noreferrer">GitHub</a><a href="https://github.com/astercloud/asterrouter/releases" target="_blank" rel="noreferrer">Releases</a><RouterLink :to="entryPath">{{ t('marketing.enterConsole') }}</RouterLink></nav>
      </div>
      <div class="marketing-shell footer-bottom"><span>© {{ new Date().getFullYear() }} AsterRouter</span><span>{{ t('marketing.footer.boundary') }}</span></div>
    </footer>
  </div>
</template>

<style scoped>
.marketing-page { --m-ink: #121926; --m-text: #344054; --m-muted: #667085; --m-line: rgba(18, 25, 38, .12); --m-paper: #fff; --m-page: #f5f7fa; --m-teal: #087f76; --m-blue: #175cd3; --m-coral: #c2413b; min-width: 320px; overflow-x: hidden; background: var(--m-paper); color: var(--m-ink); font-family: "Avenir Next", "Segoe UI Variable", Inter, "PingFang SC", "Microsoft YaHei", sans-serif; font-size: 16px; line-height: 1.6; letter-spacing: 0; color-scheme: light; }
.marketing-page *, .marketing-page *::before, .marketing-page *::after { box-sizing: border-box; }
.marketing-page a { color: inherit; text-decoration: none; }
.marketing-page button, .marketing-page select, .marketing-page a { letter-spacing: 0; }
.marketing-page :where(a, button, select):focus-visible { outline: 3px solid #2e90fa; outline-offset: 3px; }
.marketing-shell { width: min(1180px, calc(100% - 48px)); margin-inline: auto; }
.marketing-header { position: fixed; z-index: 60; inset: 0 0 auto; border-bottom: 1px solid rgba(18, 25, 38, .08); background: rgba(232, 251, 247, .88); color: var(--m-ink); backdrop-filter: blur(18px); transition: background-color 180ms ease, box-shadow 180ms ease; }
.marketing-header.scrolled, .marketing-header.open { background: rgba(255, 255, 255, .98); box-shadow: 0 8px 28px rgba(18, 25, 38, .07); }
.header-inner { display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; gap: 26px; height: 72px; }
.marketing-brand { display: inline-flex; width: max-content; align-items: center; gap: 10px; }
.marketing-brand-mark { display: grid; width: 34px; height: 34px; place-items: center; border-radius: 7px; background: #0f766e; color: #fff; font-size: 11px; font-weight: 850; box-shadow: inset 0 0 0 1px rgba(255, 255, 255, .18); }
.marketing-brand > span:last-child { display: flex; flex-direction: column; line-height: 1.05; }
.marketing-brand strong { font-size: 15px; font-weight: 800; }
.marketing-brand small { margin-top: 5px; color: var(--m-muted); font-size: 9px; font-weight: 650; }
.desktop-nav { display: flex; align-items: center; gap: 23px; white-space: nowrap; }
.desktop-nav a { color: currentColor; font-size: 12px; font-weight: 650; opacity: .76; }
.desktop-nav a:hover { opacity: 1; }
.header-actions { justify-self: end; display: flex; align-items: center; gap: 12px; }
.marketing-locale { display: inline-flex; align-items: center; gap: 5px; }
.marketing-locale select { max-width: 112px; border: 0; background: transparent; color: inherit; font-size: 11px; font-weight: 650; cursor: pointer; }
.marketing-locale option { color: var(--m-ink); }
.header-entry { min-height: 40px; display: inline-flex; align-items: center; gap: 7px; padding: 0 15px; border-radius: 999px; background: var(--m-ink); color: #fff !important; font-size: 12px; font-weight: 750; }
.mobile-menu-button { display: none; width: 40px; height: 40px; place-items: center; border: 1px solid currentColor; border-radius: 50%; background: transparent; color: inherit; }
.mobile-nav { display: none; }
.marketing-hero { position: relative; height: min(760px, calc(100svh - 28px)); min-height: 720px; overflow: hidden; border-bottom: 1px solid var(--m-line); background: #e8fbf7; color: var(--m-ink); isolation: isolate; }
.hero-content { position: relative; z-index: 2; display: flex; height: 100%; max-width: 920px; flex-direction: column; align-items: center; justify-content: flex-start; padding-top: 126px; text-align: center; }
.hero-overline { display: inline-flex; align-items: center; gap: 8px; padding: 6px 11px; border: 1px solid rgba(8, 127, 118, .18); border-radius: 999px; background: rgba(255, 255, 255, .66); color: var(--m-teal); font-size: 9px; font-weight: 800; }
.marketing-hero h1 { margin: 18px 0 0; font-size: 62px; line-height: 1; font-weight: 820; letter-spacing: 0; }
.hero-category { display: block; max-width: 760px; margin-top: 14px; font-size: 39px; line-height: 1.18; font-weight: 720; }
.hero-content > p { max-width: 720px; margin: 17px 0 0; color: var(--m-muted); font-size: 15px; line-height: 1.75; }
.hero-actions, .marketing-cta > div > div { display: flex; flex-wrap: wrap; justify-content: center; gap: 12px; margin-top: 24px; }
.primary-action, .secondary-action { min-height: 50px; display: inline-flex; align-items: center; justify-content: center; gap: 9px; padding: 0 21px; border: 1px solid; border-radius: 999px; font-size: 13px; font-weight: 750; }
.primary-action { border-color: var(--m-ink); background: var(--m-ink); color: #fff !important; box-shadow: 0 16px 36px rgba(18, 25, 38, .16); }
.secondary-action { border-color: var(--m-line); background: #fff; color: var(--m-ink) !important; }
.hero-facts { display: flex; flex-wrap: wrap; justify-content: center; gap: 8px 22px; margin-top: 18px; color: var(--m-muted); font-size: 10px; }
.hero-facts span { display: flex; align-items: center; gap: 8px; }
.hero-facts svg { color: var(--m-teal); }
.marketing-section { padding: 104px 0; }
.section-heading { max-width: 720px; margin-bottom: 46px; }
.section-heading.compact { max-width: 760px; }
.section-heading > span, .marketing-cta > div > span { color: var(--m-blue); font-size: 10px; font-weight: 850; }
.section-heading h2, .marketing-cta h2 { margin: 12px 0 0; font-size: 45px; line-height: 1.2; font-weight: 770; letter-spacing: 0; }
.section-heading p, .marketing-cta p { margin: 16px 0 0; color: var(--m-muted); font-size: 15px; line-height: 1.8; }
.decision-section { padding-top: 26px; background: #fff; }
.decision-workbench { display: grid; grid-template-columns: 280px minmax(0, 1fr); overflow: hidden; border: 1px solid var(--m-line); border-radius: 8px; background: #fff; box-shadow: 0 24px 70px rgba(18, 25, 38, .1); }
.decision-stage-list { padding: 18px; border-right: 1px solid var(--m-line); background: #f4f7f9; }
.decision-stage-list > div { min-height: 74px; display: grid; grid-template-columns: 26px 25px minmax(0, 1fr); align-items: center; gap: 8px; padding: 10px; border-bottom: 1px solid var(--m-line); color: var(--m-muted); }
.decision-stage-list > div.current { margin-inline: -4px; padding-inline: 14px; border: 1px solid rgba(23, 92, 211, .2); border-radius: 6px; background: #fff; color: var(--m-blue); box-shadow: 0 8px 22px rgba(18, 25, 38, .06); }
.decision-stage-list > div > span { font: 9px "SFMono-Regular", "Cascadia Code", monospace; }
.decision-stage-list p { margin: 0; }
.decision-stage-list strong, .decision-stage-list small { display: block; }
.decision-stage-list strong { color: var(--m-ink); font-size: 11px; }
.decision-stage-list small { margin-top: 2px; font-size: 8px; }
.decision-trace { min-width: 0; padding: 27px; }
.decision-trace > header { min-height: 54px; display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; border-bottom: 1px solid var(--m-line); }
.decision-trace > header small, .decision-trace > header strong { display: block; }
.decision-trace > header small { color: var(--m-muted); font-size: 8px; }
.decision-trace > header strong { margin-top: 2px; font: 12px "SFMono-Regular", "Cascadia Code", monospace; }
.decision-trace > header > span { display: inline-flex; align-items: center; gap: 7px; color: var(--m-teal); font-size: 9px; font-weight: 800; }
.decision-trace > header i { width: 7px; height: 7px; border-radius: 50%; background: #12b76a; box-shadow: 0 0 0 4px rgba(18, 183, 106, .12); }
.request-context { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin-top: 20px; border: 1px solid var(--m-line); border-radius: 6px; background: #f8fafc; }
.request-context > div { min-width: 0; padding: 12px 14px; border-right: 1px solid var(--m-line); }
.request-context > div:last-child { border-right: 0; }
.request-context span, .request-context strong { display: block; }
.request-context span { color: var(--m-muted); font-size: 8px; }
.request-context strong { margin-top: 5px; overflow: hidden; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.candidate-heading { display: flex; align-items: center; justify-content: space-between; gap: 18px; margin-top: 24px; padding: 0 11px 8px; color: var(--m-muted); font-size: 9px; font-weight: 750; }
.candidate-row { min-height: 66px; display: grid; grid-template-columns: 30px minmax(0, 1fr) auto; align-items: center; gap: 11px; padding: 9px 12px; border-top: 1px solid var(--m-line); }
.candidate-row > span { color: var(--m-muted); font: 9px "SFMono-Regular", "Cascadia Code", monospace; }
.candidate-row p { margin: 0; }
.candidate-row strong, .candidate-row small { display: block; }
.candidate-row strong { font-size: 11px; }
.candidate-row small { margin-top: 2px; color: var(--m-muted); font-size: 8px; }
.candidate-row > b { color: var(--m-muted); font-size: 8px; }
.candidate-row.selected { border-color: rgba(8, 127, 118, .2); background: #edf9f7; }
.candidate-row.selected > b { color: var(--m-teal); }
.candidate-row.excluded { color: #667085; }
.candidate-row.excluded > b { color: var(--m-coral); }
.decision-trace > footer { min-height: 58px; display: grid; grid-template-columns: 24px minmax(0, 1fr) 18px; align-items: center; gap: 9px; margin-top: 18px; padding: 0 12px; border-top: 1px solid var(--m-line); border-bottom: 1px solid var(--m-line); color: var(--m-blue); }
.decision-trace > footer strong, .decision-trace > footer small { display: block; }
.decision-trace > footer strong { color: var(--m-ink); font-size: 10px; }
.decision-trace > footer small { margin-top: 2px; color: var(--m-muted); font-size: 8px; }
.capability-section { background: var(--m-page); }
.capability-table { border: 1px solid var(--m-line); border-radius: 8px; overflow: hidden; background: #fff; }
.capability-head { min-height: 45px; display: grid; grid-template-columns: minmax(240px, .8fr) minmax(280px, 1fr) minmax(280px, 1fr); align-items: center; gap: 20px; padding: 0 24px 0 76px; border-bottom: 1px solid var(--m-line); background: #eef2f6; color: #5d6677; font-size: 9px; font-weight: 800; }
.capability-table article { min-height: 94px; display: grid; grid-template-columns: 34px minmax(190px, .8fr) minmax(240px, 1fr) minmax(240px, 1fr); align-items: center; gap: 18px; padding: 16px 24px; border-bottom: 1px solid var(--m-line); }
.capability-table article:last-child { border-bottom: 0; }
.capability-index { color: #667085; font: 9px "SFMono-Regular", "Cascadia Code", monospace; }
.capability-name { display: flex; align-items: center; gap: 10px; color: var(--m-blue); }
.capability-name strong { color: var(--m-ink); font-size: 12px; }
.capability-table article p { margin: 0; color: var(--m-muted); font-size: 10px; line-height: 1.65; }
.routing-section { background: #fff; }
.routing-layout { display: grid; grid-template-columns: minmax(0, .8fr) minmax(520px, 1.2fr); gap: 70px; align-items: start; }
.inline-link { display: inline-flex; align-items: center; gap: 7px; margin-top: 24px; color: var(--m-blue) !important; font-size: 12px; font-weight: 800; }
.routing-contract { overflow: hidden; border: 1px solid var(--m-line); border-radius: 8px; background: #fff; box-shadow: 0 20px 58px rgba(18, 25, 38, .08); }
.routing-contract-head { min-height: 70px; display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 0 22px; border-bottom: 1px solid var(--m-line); background: #121926; color: #fff; }
.routing-contract-head span { color: #78e2d8; font-size: 9px; font-weight: 850; }
.routing-contract-head strong { font-size: 10px; }
.preference-row { min-height: 77px; display: grid; grid-template-columns: 38px minmax(0, 1fr); align-items: center; gap: 12px; padding: 11px 22px; border-bottom: 1px solid var(--m-line); }
.preference-row > span { color: var(--m-blue); font: 9px "SFMono-Regular", "Cascadia Code", monospace; }
.preference-row p { margin: 0; }
.preference-row strong, .preference-row small { display: block; }
.preference-row strong { font-size: 12px; }
.preference-row small { margin-top: 3px; color: var(--m-muted); font-size: 9px; }
.routing-contract footer { min-height: 68px; display: grid; grid-template-columns: 25px minmax(0, 1fr); align-items: center; gap: 10px; padding: 10px 22px; background: #f5f8fb; color: var(--m-teal); }
.routing-contract footer strong, .routing-contract footer small { display: block; }
.routing-contract footer strong { color: var(--m-ink); font-size: 10px; }
.routing-contract footer small { margin-top: 2px; color: var(--m-muted); font-size: 8px; }
.governance-section { background: #121926; color: #fff; }
.governance-heading > span { color: #78e2d8; }
.governance-heading p { color: rgba(255, 255, 255, .58); }
.governance-map { display: grid; grid-template-columns: minmax(0, 1fr) 34px minmax(210px, .68fr) 34px minmax(0, 1fr); align-items: center; gap: 18px; }
.governance-map > div { min-height: 160px; display: flex; flex-direction: column; justify-content: center; padding: 24px; border: 1px solid rgba(255, 255, 255, .16); border-radius: 8px; }
.governance-map > svg { justify-self: center; color: #78e2d8; }
.governance-map small { color: #78e2d8; font-size: 9px; }
.governance-map strong { margin-top: 11px; font-size: 16px; }
.governance-map span { margin-top: 7px; color: rgba(255, 255, 255, .58); font-size: 10px; }
.governance-map .router-core { align-items: center; border-color: #13b8aa; background: #0b4d49; text-align: center; box-shadow: 0 18px 44px rgba(0, 0, 0, .2); }
.router-core .marketing-brand-mark { color: #fff; }
.router-core strong { margin-top: 12px; }
.router-core small { margin-top: 5px; color: rgba(255, 255, 255, .62); }
.governance-facts { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 18px; margin-top: 32px; padding-top: 22px; border-top: 1px solid rgba(255, 255, 255, .12); }
.governance-facts span { display: flex; align-items: flex-start; gap: 8px; color: rgba(255, 255, 255, .64); font-size: 10px; }
.governance-facts svg { flex: 0 0 auto; color: #78e2d8; }
.marketing-cta { padding: 100px 0; background: #edf4ff; text-align: center; }
.marketing-cta > div { display: flex; flex-direction: column; align-items: center; }
.marketing-cta h2 { max-width: 780px; }
.marketing-cta p { max-width: 690px; }
.marketing-cta p { color: #5d6677; }
.marketing-cta .primary-action { color: #fff !important; }
.marketing-cta .secondary-action { border-color: var(--m-line); background: #fff; color: var(--m-ink) !important; }
.marketing-footer { padding: 62px 0 22px; background: #0a101a; color: rgba(255, 255, 255, .55); }
.footer-main { display: grid; grid-template-columns: 1.5fr .7fr .7fr; gap: 64px; }
.footer-brand { color: #fff !important; }
.footer-brand small { color: rgba(255, 255, 255, .5) !important; }
.footer-main > div > p { max-width: 340px; margin: 18px 0 0; font-size: 11px; }
.footer-main nav { display: flex; flex-direction: column; gap: 9px; font-size: 10px; }
.footer-main nav strong { margin-bottom: 5px; color: #fff; font-size: 10px; }
.footer-main nav a:hover { color: #78e2d8; }
.footer-bottom { display: flex; justify-content: space-between; gap: 20px; margin-top: 50px; padding-top: 17px; border-top: 1px solid rgba(255, 255, 255, .1); font: 8px "SFMono-Regular", "Cascadia Code", monospace; }
@media (max-width: 1080px) {
  .header-inner { grid-template-columns: 1fr auto; }
  .desktop-nav, .header-actions { display: none; }
  .mobile-menu-button { display: grid; }
  .mobile-nav { display: flex; flex-direction: column; gap: 3px; padding: 9px 24px 17px; border-top: 1px solid var(--m-line); background: rgba(255, 255, 255, .98); color: var(--m-ink); }
  .mobile-nav a { min-height: 43px; display: flex; align-items: center; justify-content: space-between; padding: 0 7px; font-size: 12px; font-weight: 700; }
  .mobile-locale { min-height: 43px; display: flex; align-items: center; gap: 9px; padding: 0 7px; border-top: 1px solid var(--m-line); }
  .mobile-locale select { flex: 1; border: 0; background: transparent; color: var(--m-ink); font-size: 12px; font-weight: 700; }
  .routing-layout { grid-template-columns: 1fr; gap: 20px; }
  .routing-layout .section-heading { max-width: 760px; }
  .marketing-hero { height: 780px; min-height: 780px; }
  .hero-content { height: auto; justify-content: flex-start; padding: 108px 24px 0; }
  .hero-category, .hero-content > p { max-width: 700px; }
}
@media (max-width: 820px) {
  .marketing-shell { width: min(100% - 32px, 1180px); }
  .marketing-hero { height: 800px; min-height: 800px; }
  .marketing-hero h1 { font-size: 48px; }
  .hero-category { max-width: 560px; font-size: 31px; }
  .hero-content > p { max-width: 560px; font-size: 15px; }
  .marketing-section { padding: 80px 0; }
  .decision-section { padding-top: 26px; }
  .section-heading h2, .marketing-cta h2 { font-size: 36px; }
  .decision-workbench { grid-template-columns: 1fr; }
  .decision-stage-list { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); padding: 10px; border-right: 0; border-bottom: 1px solid var(--m-line); }
  .decision-stage-list > div, .decision-stage-list > div.current { min-height: 86px; grid-template-columns: 1fr; justify-items: center; align-content: center; gap: 4px; margin: 0; padding: 7px 4px; border: 0; border-right: 1px solid var(--m-line); border-radius: 0; background: transparent; box-shadow: none; text-align: center; }
  .decision-stage-list > div:last-child { border-right: 0; }
  .decision-stage-list small { display: none; }
  .decision-trace { padding: 20px; }
  .capability-head { display: none; }
  .capability-table article { grid-template-columns: 32px minmax(0, 1fr); gap: 7px 12px; padding: 18px; }
  .capability-name, .capability-table article p { grid-column: 2; }
  .capability-index { grid-row: 1 / 4; align-self: start; padding-top: 3px; }
  .governance-map { grid-template-columns: 1fr; }
  .governance-map > svg { transform: rotate(90deg); }
  .governance-map > div { min-height: 125px; }
  .governance-facts { grid-template-columns: 1fr; }
}
@media (max-width: 560px) {
  .header-inner { height: 66px; }
  .marketing-brand small { display: none; }
  .marketing-hero { height: 810px; min-height: 810px; }
  .hero-content { padding: 92px 16px 0; }
  .hero-overline { font-size: 8px; }
  .marketing-hero h1 { margin-top: 15px; font-size: 40px; }
  .hero-category { margin-top: 12px; font-size: 25px; }
  .hero-content > p { margin-top: 13px; font-size: 13px; line-height: 1.65; }
  .hero-actions { width: 100%; max-width: 358px; margin-top: 17px; }
  .primary-action, .secondary-action { min-height: 44px; padding-inline: 15px; font-size: 11px; }
  .hero-facts { align-items: center; flex-direction: column; gap: 4px; margin-top: 13px; font-size: 9px; }
  .section-heading h2, .marketing-cta h2 { font-size: 32px; }
  .section-heading p, .marketing-cta p { font-size: 14px; }
  .decision-stage-list { grid-template-columns: 1fr; padding: 8px 16px; }
  .decision-stage-list > div, .decision-stage-list > div.current { min-height: 46px; grid-template-columns: 24px 22px minmax(0, 1fr); justify-items: start; border-right: 0; border-bottom: 1px solid var(--m-line); text-align: left; }
  .decision-stage-list > div:last-child { border-bottom: 0; }
  .decision-stage-list p { min-width: 0; }
  .decision-trace { padding: 16px; }
  .decision-trace > header { flex-direction: column; padding-bottom: 13px; }
  .request-context { grid-template-columns: 1fr; }
  .request-context > div { border-right: 0; border-bottom: 1px solid var(--m-line); }
  .request-context > div:last-child { border-bottom: 0; }
  .candidate-heading small { display: none; }
  .candidate-row { grid-template-columns: 24px minmax(0, 1fr); }
  .candidate-row > b { grid-column: 2; }
  .routing-contract-head { align-items: flex-start; flex-direction: column; justify-content: center; gap: 3px; }
  .routing-layout { gap: 5px; }
  .footer-main { grid-template-columns: 1fr; gap: 34px; }
  .footer-bottom { align-items: flex-start; flex-direction: column; }
}
@media (prefers-reduced-motion: reduce) {
  .marketing-page *, .marketing-page *::before, .marketing-page *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; }
}
</style>
