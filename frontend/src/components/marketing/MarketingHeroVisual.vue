<script setup lang="ts">
import {
  Check,
  CircleDollarSign,
  Gauge,
  Route,
  ShieldCheck,
  SquareActivity
} from '@lucide/vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <div class="marketing-home-scene">
    <div class="hero-image-stage">
      <img
        class="hero-product-image"
        src="/images/asterrouter-routing-workbench.webp"
        :alt="t('marketing.hero.imageAlt')"
      />
    </div>

    <section class="hero-decision-window" :aria-label="t('marketing.hero.liveDecisionLabel')">
      <header>
        <span><SquareActivity :size="15" />{{ t('marketing.hero.liveDecision') }}</span>
        <b><i></i>LIVE</b>
      </header>
      <div class="hero-request-heading">
        <span>req_7f2c8a19</span>
        <strong>Customer Support Copilot</strong>
        <small>enterprise-chat · production</small>
      </div>
      <div class="hero-candidate selected">
        <span>01</span>
        <p><strong>Primary East</strong><small>{{ t('marketing.decision.candidatePrimary') }}</small></p>
        <b>{{ t('marketing.decision.selected') }}</b>
      </div>
      <div class="hero-candidate excluded">
        <span>03</span>
        <p><strong>Overflow Pool</strong><small>{{ t('marketing.decision.candidateExcluded') }}</small></p>
        <b>{{ t('marketing.decision.excluded') }}</b>
      </div>
      <footer>
        <Check :size="15" />
        <span><strong>{{ t('marketing.hero.requestApproved') }}</strong><small>{{ t('marketing.hero.requestApprovedDetail') }}</small></span>
      </footer>
    </section>

    <div class="hero-signal is-policy">
      <ShieldCheck :size="17" />
      <span><strong>{{ t('marketing.hero.policyProof') }}</strong><small>{{ t('marketing.hero.policyProofDetail') }}</small></span>
    </div>
    <div class="hero-signal is-cost">
      <CircleDollarSign :size="17" />
      <span><strong>{{ t('marketing.hero.costProof') }}</strong><small>{{ t('marketing.hero.costProofDetail') }}</small></span>
    </div>
    <div class="hero-signal is-capacity">
      <Gauge :size="17" />
      <span><strong>{{ t('marketing.hero.capacityProof') }}</strong><small>{{ t('marketing.hero.capacityProofDetail') }}</small></span>
    </div>
    <div class="hero-route-proof" aria-hidden="true">
      <Route :size="16" /><span>Policy</span><i></i><span>Supply</span><i></i><span>Trace</span>
    </div>
  </div>
</template>

<style scoped>
.marketing-home-scene {
  position: absolute;
  z-index: 0;
  top: 102px;
  left: 50%;
  width: min(720px, calc(50% - 24px));
  height: 590px;
}

.hero-image-stage {
  position: absolute;
  inset: 34px 0 68px 30px;
  overflow: hidden;
  border: 1px solid rgba(18, 25, 38, .16);
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 30px 72px rgba(18, 37, 54, .17);
}

.hero-image-stage::before {
  position: absolute;
  z-index: 1;
  inset: 0 0 auto;
  height: 30px;
  border-bottom: 1px solid rgba(18, 25, 38, .09);
  background: rgba(255, 255, 255, .94);
  content: '';
}

.hero-image-stage::after {
  position: absolute;
  z-index: 2;
  top: 11px;
  left: 13px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #d95c55;
  box-shadow: 10px 0 #e9b949, 20px 0 #43a879;
  content: '';
}

.hero-product-image {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: left top;
}

.hero-decision-window {
  position: absolute;
  z-index: 4;
  right: -18px;
  bottom: 0;
  width: 390px;
  overflow: hidden;
  border: 1px solid rgba(18, 25, 38, .15);
  border-radius: 8px;
  background: rgba(255, 255, 255, .97);
  box-shadow: 0 24px 58px rgba(18, 37, 54, .18);
  backdrop-filter: blur(16px);
}

.hero-decision-window > header {
  min-height: 45px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 0 14px;
  border-bottom: 1px solid rgba(18, 25, 38, .1);
}

.hero-decision-window > header > span,
.hero-decision-window > header > b {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 9px;
  font-weight: 800;
}

.hero-decision-window > header > span { color: #121926; }
.hero-decision-window > header > span svg { color: #175cd3; }
.hero-decision-window > header > b { color: #087f76; }
.hero-decision-window > header i { width: 6px; height: 6px; border-radius: 50%; background: #12b76a; box-shadow: 0 0 0 4px rgba(18, 183, 106, .11); }

.hero-request-heading { padding: 13px 14px 10px; }
.hero-request-heading span,
.hero-request-heading strong,
.hero-request-heading small { display: block; }
.hero-request-heading span { color: #087f76; font: 8px "SFMono-Regular", "Cascadia Code", monospace; }
.hero-request-heading strong { margin-top: 4px; color: #121926; font-size: 12px; }
.hero-request-heading small { margin-top: 2px; color: #667085; font-size: 8px; }

.hero-candidate {
  min-height: 51px;
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr) auto;
  align-items: center;
  gap: 9px;
  padding: 7px 14px;
  border-top: 1px solid rgba(18, 25, 38, .09);
}

.hero-candidate > span { color: #667085; font: 8px "SFMono-Regular", "Cascadia Code", monospace; }
.hero-candidate p { min-width: 0; margin: 0; }
.hero-candidate strong,
.hero-candidate small { display: block; }
.hero-candidate strong { color: #121926; font-size: 9px; }
.hero-candidate small { margin-top: 2px; overflow: hidden; color: #667085; font-size: 7px; text-overflow: ellipsis; white-space: nowrap; }
.hero-candidate > b { color: #667085; font-size: 7px; }
.hero-candidate.selected { border-color: rgba(8, 127, 118, .18); background: #edf9f7; }
.hero-candidate.selected > b { color: #087f76; }
.hero-candidate.excluded > b { color: #c2413b; }

.hero-decision-window > footer {
  min-height: 50px;
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  border-top: 1px solid rgba(18, 25, 38, .09);
  color: #175cd3;
}

.hero-decision-window > footer strong,
.hero-decision-window > footer small { display: block; }
.hero-decision-window > footer strong { color: #121926; font-size: 9px; }
.hero-decision-window > footer small { margin-top: 2px; color: #667085; font-size: 7px; }

.hero-signal {
  position: absolute;
  z-index: 5;
  min-width: 190px;
  min-height: 58px;
  display: grid;
  grid-template-columns: 26px minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  padding: 9px 12px;
  border: 1px solid rgba(18, 25, 38, .14);
  border-radius: 8px;
  background: rgba(255, 255, 255, .97);
  box-shadow: 0 16px 40px rgba(18, 37, 54, .12);
  backdrop-filter: blur(14px);
}

.hero-signal svg { color: #175cd3; }
.hero-signal strong,
.hero-signal small { display: block; }
.hero-signal strong { color: #121926; font-size: 9px; }
.hero-signal small { margin-top: 2px; color: #667085; font-size: 7px; }
.hero-signal.is-policy { top: 0; left: 0; }
.hero-signal.is-cost { top: 90px; right: -4px; }
.hero-signal.is-cost svg { color: #b54708; }
.hero-signal.is-capacity { bottom: 10px; left: 8px; }
.hero-signal.is-capacity svg { color: #087f76; }

.hero-route-proof {
  position: absolute;
  z-index: 3;
  top: 102px;
  left: 12px;
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 7px 10px;
  border: 1px solid rgba(255, 255, 255, .18);
  border-radius: 6px;
  background: #121926;
  color: rgba(255, 255, 255, .7);
  font-size: 7px;
  font-weight: 750;
  box-shadow: 0 12px 30px rgba(18, 25, 38, .2);
}

.hero-route-proof svg { color: #78e2d8; }
.hero-route-proof i { width: 13px; height: 1px; background: rgba(120, 226, 216, .55); }

@media (max-width: 1080px) {
  .marketing-home-scene { top: auto; right: 24px; bottom: 38px; left: 24px; width: auto; height: 330px; }
  .hero-image-stage { inset: 0 12% 10px 0; }
  .hero-decision-window { right: 0; width: min(390px, 62%); }
  .hero-signal.is-policy { top: -24px; left: auto; right: 18px; }
  .hero-signal.is-cost,
  .hero-signal.is-capacity,
  .hero-route-proof { display: none; }
}

@media (max-width: 560px) {
  .marketing-home-scene { right: 16px; bottom: 34px; left: 16px; height: 275px; }
  .hero-image-stage { inset: 0 0 12px; }
  .hero-product-image { object-position: 18% top; }
  .hero-decision-window { right: 8px; bottom: 0; width: calc(100% - 16px); }
  .hero-decision-window > header,
  .hero-request-heading,
  .hero-candidate { padding-right: 11px; padding-left: 11px; }
  .hero-candidate.excluded { display: none; }
  .hero-signal.is-policy { display: none; }
}

@media (prefers-reduced-motion: no-preference) {
  .hero-image-stage { animation: hero-stage-enter 520ms ease-out both; }
  .hero-decision-window { animation: hero-panel-enter 600ms 80ms ease-out both; }
}

@keyframes hero-stage-enter {
  from { opacity: 0; transform: translateY(14px); }
  to { opacity: 1; transform: translateY(0); }
}

@keyframes hero-panel-enter {
  from { opacity: 0; transform: translateY(12px); }
  to { opacity: 1; transform: translateY(0); }
}
</style>
