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

    <section class="hero-decision-strip" :aria-label="t('marketing.hero.liveDecisionLabel')">
      <div class="hero-request-identity">
        <span>
          <SquareActivity :size="15" />
          {{ t('marketing.hero.liveDecision') }}
          <b><i></i>LIVE</b>
        </span>
        <strong>Customer Support Copilot</strong>
        <small>req_7f2c8a19 · enterprise-chat · production</small>
      </div>

      <div class="hero-proof-list">
        <div>
          <ShieldCheck :size="17" />
          <span><strong>{{ t('marketing.hero.policyProof') }}</strong><small>{{ t('marketing.hero.policyProofDetail') }}</small></span>
        </div>
        <div>
          <CircleDollarSign :size="17" />
          <span><strong>{{ t('marketing.hero.costProof') }}</strong><small>{{ t('marketing.hero.costProofDetail') }}</small></span>
        </div>
        <div>
          <Gauge :size="17" />
          <span><strong>{{ t('marketing.hero.capacityProof') }}</strong><small>{{ t('marketing.hero.capacityProofDetail') }}</small></span>
        </div>
      </div>

      <footer>
        <Check :size="17" />
        <span><strong>{{ t('marketing.hero.requestApproved') }}</strong><small>{{ t('marketing.hero.requestApprovedDetail') }}</small></span>
        <Route :size="17" />
      </footer>
    </section>
  </div>
</template>

<style scoped>
.marketing-home-scene {
  position: absolute;
  z-index: 0;
  inset: 0;
  overflow: hidden;
  pointer-events: none;
}

.hero-image-stage {
  position: absolute;
  top: 535px;
  left: 50%;
  width: min(1120px, calc(100% - 72px));
  height: 520px;
  overflow: hidden;
  transform: translateX(-50%);
  border: 1px solid rgba(18, 25, 38, .15);
  border-radius: 8px 8px 0 0;
  background: #fff;
  box-shadow: 0 30px 72px rgba(18, 37, 54, .17);
}

.hero-image-stage::before {
  position: absolute;
  z-index: 1;
  inset: 0 0 auto;
  height: 30px;
  border-bottom: 1px solid rgba(18, 25, 38, .09);
  background: rgba(255, 255, 255, .96);
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

.hero-decision-strip {
  position: absolute;
  z-index: 4;
  top: 490px;
  left: 50%;
  width: min(900px, calc(100% - 120px));
  min-height: 112px;
  display: grid;
  grid-template-columns: minmax(210px, .8fr) minmax(360px, 1.5fr) minmax(220px, .9fr);
  align-items: stretch;
  transform: translateX(-50%);
  overflow: hidden;
  border: 1px solid rgba(18, 25, 38, .13);
  border-radius: 8px;
  background: rgba(255, 255, 255, .98);
  box-shadow: 0 24px 58px rgba(18, 37, 54, .16);
  backdrop-filter: blur(16px);
}

.hero-request-identity,
.hero-proof-list,
.hero-decision-strip > footer {
  min-width: 0;
  padding: 16px 18px;
}

.hero-request-identity {
  display: flex;
  flex-direction: column;
  justify-content: center;
  border-right: 1px solid rgba(18, 25, 38, .1);
}

.hero-request-identity > span {
  display: flex;
  align-items: center;
  gap: 6px;
  color: #175cd3;
  font-size: 8px;
  font-weight: 800;
}

.hero-request-identity > span b {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin-left: auto;
  color: #087f76;
  font-size: 7px;
}

.hero-request-identity > span i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #12b76a;
  box-shadow: 0 0 0 4px rgba(18, 183, 106, .11);
}

.hero-request-identity > strong,
.hero-request-identity > small,
.hero-proof-list strong,
.hero-proof-list small,
.hero-decision-strip > footer strong,
.hero-decision-strip > footer small {
  display: block;
}

.hero-request-identity > strong {
  margin-top: 10px;
  overflow: hidden;
  color: #121926;
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hero-request-identity > small {
  margin-top: 3px;
  overflow: hidden;
  color: #667085;
  font: 7px "SFMono-Regular", "Cascadia Code", monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hero-proof-list {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: center;
  gap: 0;
  border-right: 1px solid rgba(18, 25, 38, .1);
}

.hero-proof-list > div {
  min-width: 0;
  display: grid;
  grid-template-columns: 22px minmax(0, 1fr);
  align-items: center;
  gap: 6px;
  padding-inline: 10px;
  border-right: 1px solid rgba(18, 25, 38, .08);
}

.hero-proof-list > div:last-child { border-right: 0; }
.hero-proof-list svg { color: #175cd3; }
.hero-proof-list > div:nth-child(2) svg { color: #b54708; }
.hero-proof-list > div:nth-child(3) svg { color: #087f76; }
.hero-proof-list strong { color: #121926; font-size: 8px; line-height: 1.35; }
.hero-proof-list small { margin-top: 3px; color: #667085; font-size: 6px; line-height: 1.4; }

.hero-decision-strip > footer {
  display: grid;
  grid-template-columns: 22px minmax(0, 1fr) 18px;
  align-items: center;
  gap: 7px;
  background: #edf9f7;
  color: #087f76;
}

.hero-decision-strip > footer strong { color: #121926; font-size: 9px; line-height: 1.35; }
.hero-decision-strip > footer small { margin-top: 3px; color: #667085; font-size: 6px; line-height: 1.4; }

@media (max-width: 1080px) {
  .hero-image-stage { top: 555px; width: calc(100% - 48px); }
  .hero-decision-strip {
    top: 510px;
    width: min(820px, calc(100% - 72px));
    grid-template-columns: minmax(190px, .8fr) minmax(340px, 1.5fr) minmax(190px, .85fr);
  }
  .hero-request-identity,
  .hero-proof-list,
  .hero-decision-strip > footer { padding-inline: 14px; }
}

@media (max-width: 820px) {
  .hero-image-stage { top: 575px; width: calc(100% - 32px); height: 400px; }
  .hero-decision-strip {
    top: 530px;
    width: calc(100% - 48px);
    grid-template-columns: minmax(0, 1.35fr) minmax(190px, .8fr);
  }
  .hero-request-identity { display: none; }
  .hero-proof-list { border-left: 0; }
}

@media (max-width: 560px) {
  .hero-image-stage { top: 650px; width: calc(100% - 24px); height: 300px; }
  .hero-product-image { object-position: 15% top; }
  .hero-decision-strip {
    top: 595px;
    width: calc(100% - 32px);
    min-height: 118px;
    grid-template-columns: 1fr;
  }
  .hero-proof-list {
    min-height: 61px;
    padding: 8px;
    border-right: 0;
    border-bottom: 1px solid rgba(18, 25, 38, .1);
  }
  .hero-proof-list > div { grid-template-columns: 1fr; justify-items: center; padding-inline: 6px; text-align: center; }
  .hero-proof-list small { display: none; }
  .hero-decision-strip > footer { min-height: 56px; padding: 8px 12px; }
}

@media (prefers-reduced-motion: no-preference) {
  .hero-image-stage { animation: hero-stage-enter 520ms ease-out both; }
  .hero-decision-strip { animation: hero-strip-enter 600ms 80ms ease-out both; }
}

@keyframes hero-stage-enter {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes hero-strip-enter {
  from { opacity: 0; margin-top: 12px; }
  to { opacity: 1; margin-top: 0; }
}
</style>
