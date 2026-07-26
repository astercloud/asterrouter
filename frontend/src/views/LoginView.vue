<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Eye, EyeOff, Lock, LogIn, Mail, Play, UserRound } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import TurnstileWidget from '@/components/TurnstileWidget.vue'
import { availableLocales, getLocale, setLocale, type LocaleCode } from '@/i18n'
import { forgotPassword, register, resendVerification, resetPassword, verifyEmail } from '@/api/auth'
import { defaultSurfaceRoute } from '@/router/surfaces'

type AuthMode = 'login' | 'register' | 'forgot' | 'reset' | 'verify' | 'resend'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()
const showPassword = ref(false)
const form = reactive({ username: 'admin', password: '' })
const accountForm = reactive({ email: '', displayName: '', password: '', confirmPassword: '', invitationCode: '' })
const mfaCode = ref('')
const mfaChallenge = ref(typeof route.query.mfa === 'string' ? route.query.mfa : '')
const actionMessage = ref('')
const registrationComplete = ref(false)
const pendingVerificationEmail = ref('')
const agreementAccepted = ref(false)
const turnstileToken = ref('')
const turnstileResetKey = ref(0)

const authMode = computed<AuthMode>(() => {
	if (route.path === '/register') return 'register'
	if (route.path === '/forgot-password') return 'forgot'
	if (route.path === '/resend-verification') return 'resend'
	if (route.path === '/reset-password' || typeof route.query.reset === 'string') return 'reset'
	if (route.path === '/verify-email' || typeof route.query.verify === 'string') return 'verify'
	return 'login'
})
const resetToken = computed(() => typeof route.query.token === 'string' ? route.query.token : typeof route.query.reset === 'string' ? route.query.reset : '')
const verificationToken = computed(() => typeof route.query.token === 'string' ? route.query.token : typeof route.query.verify === 'string' ? route.query.verify : '')
const redirectTo = computed(() => {
  const value = route.query.redirect
  if (typeof value === 'string' && value.startsWith('/')) return value
  return defaultEntry()
})
const demoMode = computed(() => Boolean(app.publicSettings?.demo_mode))
const turnstileRequired = computed(() => Boolean(app.publicSettings?.turnstile_enabled && app.publicSettings.turnstile_site_key))
const allowedDomains = computed(() => app.publicSettings?.allowed_email_domains || [])
const modeTitle = computed(() => {
	if (mfaChallenge.value) return t('auth.mfaTitle')
	return t({ login: 'auth.welcomeBack', register: 'auth.createAccount', forgot: 'auth.forgotPasswordTitle', reset: 'auth.resetPassword', verify: 'auth.verifyEmail', resend: 'auth.resendVerificationTitle' }[authMode.value])
})
const modeSubtitle = computed(() => {
	if (mfaChallenge.value) return t('auth.mfaHelp')
	return t({ login: 'auth.signInToAccount', register: 'auth.registrationHelp', forgot: 'auth.resetEmailHelp', reset: 'auth.resetPasswordHelp', verify: 'auth.verifyEmailHelp', resend: 'auth.resendVerificationHelp' }[authMode.value])
})

onMounted(async () => {
	if (typeof route.query.email === 'string') accountForm.email = route.query.email.trim()
	if (authMode.value === 'verify') {
		if (!verificationToken.value) {
			auth.error = t('auth.invalidVerificationLink')
			return
		}
		try {
			await verifyEmail(verificationToken.value)
			actionMessage.value = t('auth.emailVerified')
		} catch (err) {
			auth.error = err instanceof Error ? err.message : t('auth.invalidVerificationLink')
		}
		return
	}
	if (route.query.oidc !== 'success' && route.query.provider !== 'feishu') return
	try {
		await auth.completeOIDCLogin()
		await router.replace(defaultEntry())
	} catch {
		// The store exposes the translated API error on the form.
	}
})

function loginWithOIDC() {
  window.location.assign(`/api/v1/auth/oidc?agreement_accepted=${agreementAccepted.value}`)
}

function loginWithFeishu() { window.location.assign(`/api/v1/auth/feishu?agreement_accepted=${agreementAccepted.value}`) }
function loginWithDingTalk() { window.location.assign(`/api/v1/auth/dingtalk?agreement_accepted=${agreementAccepted.value}`) }
function loginWithSocial(provider: 'github' | 'google') { window.location.assign(`/api/v1/auth/oauth/${provider}?agreement_accepted=${agreementAccepted.value}`) }

async function submit() {
	try {
		const challenge = await auth.login(form.username, form.password, agreementAccepted.value, turnstileToken.value)
		if (challenge) {
			mfaChallenge.value = challenge.challenge
			mfaCode.value = ''
			return
		}
		await router.push(redirectTo.value)
	} catch {
		resetHumanVerification()
	}
}

async function enterDemo() {
	try {
		await auth.login('demo', 'demo')
		await router.push(redirectTo.value)
	} catch {
		// The store exposes the error on the form.
	}
}

async function submitMFA() {
	try {
		await auth.completeMFA(mfaChallenge.value, mfaCode.value)
		await router.replace(redirectTo.value)
	} catch {
		mfaCode.value = ''
	}
}

async function submitAccountAction() {
	actionMessage.value = ''
	auth.error = ''
	try {
		if (authMode.value === 'register') {
			if (accountForm.password !== accountForm.confirmPassword) throw new Error(t('auth.passwordMismatch'))
			const result = await register(accountForm.email, accountForm.password, accountForm.displayName, accountForm.invitationCode, agreementAccepted.value, turnstileToken.value)
			pendingVerificationEmail.value = accountForm.email
			registrationComplete.value = true
			actionMessage.value = result.email_delivery_failed ? t('auth.verificationDeliveryFailed') : result.verification_required ? t('auth.registrationVerifyEmail') : t('auth.registrationAccepted')
		}
		if (authMode.value === 'forgot') {
			await forgotPassword(accountForm.email, turnstileToken.value)
			actionMessage.value = t('auth.resetEmailAccepted')
		}
		if (authMode.value === 'resend') {
			await resendVerification(accountForm.email, turnstileToken.value)
			actionMessage.value = t('auth.verificationEmailAccepted')
		}
		if (authMode.value === 'reset') {
			if (!resetToken.value) throw new Error(t('auth.invalidResetLink'))
			if (accountForm.password !== accountForm.confirmPassword) throw new Error(t('auth.passwordMismatch'))
			await resetPassword(resetToken.value, accountForm.password)
			actionMessage.value = t('auth.passwordResetComplete')
			accountForm.password = ''
			accountForm.confirmPassword = ''
		}
	} catch (err) {
		auth.error = err instanceof Error ? err.message : t('common.failed')
	} finally {
		if (authMode.value === 'register' || authMode.value === 'forgot' || authMode.value === 'resend') resetHumanVerification()
	}
}

async function submitResend() {
	auth.error = ''
	actionMessage.value = ''
	try {
		await resendVerification(pendingVerificationEmail.value, turnstileToken.value)
		actionMessage.value = t('auth.verificationEmailAccepted')
	} catch (err) {
		auth.error = err instanceof Error ? err.message : t('common.failed')
	} finally {
		resetHumanVerification()
	}
}

async function goTo(path: string) {
	auth.error = ''
	actionMessage.value = ''
	registrationComplete.value = false
	mfaChallenge.value = ''
	resetHumanVerification()
	await router.push(path)
}

function resetHumanVerification() {
	turnstileToken.value = ''
	turnstileResetKey.value++
}

function defaultEntry(): string {
  const settings = app.publicSettings
  return defaultSurfaceRoute(settings?.enabled_profiles || [], settings?.default_profile || '', auth.user)
}

function changeLocale(event: Event) {
  setLocale((event.target as HTMLSelectElement).value as LocaleCode)
}
</script>

<template>
  <main class="auth-page">
    <div class="auth-bg-grid" aria-hidden="true"></div>
    <label class="auth-locale locale-control">
      <select :value="getLocale()" :aria-label="t('nav.language')" @change="changeLocale">
        <option v-for="locale in availableLocales" :key="locale.code" :value="locale.code">{{ locale.label }}</option>
      </select>
    </label>

    <div class="auth-container">
      <div class="auth-brand">
        <img v-if="app.publicSettings?.site_logo" :src="app.publicSettings.site_logo" class="auth-brand-logo" alt="" />
        <div v-else class="brand-mark large">AR</div>
        <h1>{{ app.siteName }}</h1>
        <p>{{ app.siteSubtitle }}</p>
      </div>

      <section class="auth-card">
        <div class="auth-title">
          <h2>{{ modeTitle }}</h2>
          <p>{{ modeSubtitle }}</p>
        </div>

        <section v-if="demoMode && authMode === 'login' && !mfaChallenge" class="demo-experience" aria-labelledby="demo-experience-title">
          <div class="demo-experience-copy">
            <span class="demo-experience-label">{{ t('auth.demoMode') }}</span>
            <strong id="demo-experience-title">{{ t('auth.demoModeTitle') }}</strong>
            <p>{{ t('auth.demoModeHelp') }}</p>
          </div>
          <button class="button auth-submit demo-experience-action" type="button" :disabled="auth.loading" @click="enterDemo">
            <Play :size="18" aria-hidden="true" />
            {{ auth.loading ? t('auth.demoSigningIn') : t('auth.enterDemo') }}
          </button>
        </section>

        <div v-if="demoMode && authMode === 'login' && !mfaChallenge" class="auth-divider"><span>{{ t('auth.accountSignIn') }}</span></div>
        <div v-if="actionMessage" class="notice success">{{ actionMessage }}</div>
        <div v-if="auth.error" class="notice">{{ auth.error }}</div>

        <div v-if="authMode === 'verify'" class="auth-form">
          <button class="button secondary auth-submit" type="button" @click="goTo('/login')">{{ t('auth.backToLogin') }}</button>
        </div>

        <form v-else-if="registrationComplete" class="auth-form" @submit.prevent="submitResend">
          <TurnstileWidget v-if="turnstileRequired" :site-key="app.publicSettings!.turnstile_site_key" :reset-key="turnstileResetKey" @token="turnstileToken = $event" />
          <button v-if="app.publicSettings?.email_verify_enabled" class="button auth-submit" type="submit" :disabled="turnstileRequired && !turnstileToken">
            <Mail :size="18" />{{ t('auth.resendVerification') }}
          </button>
          <button class="button secondary auth-submit" type="button" @click="goTo('/login')">{{ t('auth.backToLogin') }}</button>
        </form>

        <form v-else-if="mfaChallenge" class="auth-form" @submit.prevent="submitMFA">
          <div class="field">
            <label for="mfa-code">{{ t('auth.totpCode') }}</label>
			<div class="input-with-icon"><Lock :size="18" /><input id="mfa-code" v-model="mfaCode" inputmode="text" maxlength="13" autocomplete="one-time-code" required /></div>
          </div>
          <button class="button auth-submit" type="submit" :disabled="auth.loading"><LogIn :size="18" />{{ t('auth.verifyAndSignIn') }}</button>
          <button class="button secondary auth-submit" type="button" @click="goTo('/login')">{{ t('auth.backToLogin') }}</button>
        </form>

        <form v-else-if="authMode === 'login'" class="auth-form" @submit.prevent="submit">
          <div class="field">
            <label for="username">{{ t('auth.username') }}</label>
            <div class="input-with-icon"><UserRound :size="18" aria-hidden="true" /><input id="username" v-model="form.username" autocomplete="username" autofocus required :placeholder="t('auth.usernamePlaceholder')" /></div>
          </div>
          <div class="field">
            <label for="password">{{ t('auth.password') }}</label>
            <div class="input-with-icon">
              <Lock :size="18" aria-hidden="true" />
              <input id="password" v-model="form.password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" required :placeholder="t('auth.passwordPlaceholder')" />
              <button type="button" class="icon-button" :aria-label="showPassword ? t('auth.hidePassword') : t('auth.showPassword')" :title="showPassword ? t('auth.hidePassword') : t('auth.showPassword')" @click="showPassword = !showPassword">
                <EyeOff v-if="showPassword" :size="18" /><Eye v-else :size="18" />
              </button>
            </div>
          </div>
          <TurnstileWidget v-if="turnstileRequired" :site-key="app.publicSettings!.turnstile_site_key" :reset-key="turnstileResetKey" @token="turnstileToken = $event" />
          <label v-if="app.publicSettings?.login_agreement_enabled" class="agreement-check">
            <input v-model="agreementAccepted" type="checkbox" required />
            <span>{{ t('auth.agreementPrefix') }} <a v-for="document in app.publicSettings.legal_documents" :key="document.id" :href="`/legal/${document.slug}`" target="_blank">{{ document.name }}</a></span>
          </label>
          <button class="button auth-submit" type="submit" :disabled="auth.loading || (turnstileRequired && !turnstileToken)"><LogIn :size="18" />{{ auth.loading ? t('auth.signingIn') : t('auth.signIn') }}</button>
          <div class="auth-secondary-actions">
            <button v-if="app.publicSettings?.password_reset_enabled" type="button" @click="goTo('/forgot-password')">{{ t('auth.forgotPassword') }}</button>
						<button v-if="app.publicSettings?.email_verify_enabled" type="button" @click="goTo('/resend-verification')">{{ t('auth.resendVerification') }}</button>
            <button v-if="app.publicSettings?.registration_enabled" type="button" @click="goTo('/register')">{{ t('auth.createAccount') }}</button>
          </div>
          <button v-if="app.publicSettings?.oidc_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings.login_agreement_enabled && !agreementAccepted" @click="loginWithOIDC"><LogIn :size="18" />{{ app.publicSettings.oidc_provider_name || 'OIDC' }}</button>
          <button v-if="app.publicSettings?.feishu_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings.login_agreement_enabled && !agreementAccepted" @click="loginWithFeishu"><LogIn :size="18" />{{ app.publicSettings.feishu_region === 'global' ? 'Lark' : 'Feishu' }}</button>
          <button v-if="app.publicSettings?.github_oauth_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings.login_agreement_enabled && !agreementAccepted" @click="loginWithSocial('github')"><LogIn :size="18" />GitHub</button>
          <button v-if="app.publicSettings?.google_oauth_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings.login_agreement_enabled && !agreementAccepted" @click="loginWithSocial('google')"><LogIn :size="18" />Google</button>
          <button v-if="app.publicSettings?.dingtalk_enabled" class="button secondary auth-submit" type="button" :disabled="app.publicSettings.login_agreement_enabled && !agreementAccepted" @click="loginWithDingTalk"><LogIn :size="18" />DingTalk</button>
        </form>

        <form v-else class="auth-form" @submit.prevent="submitAccountAction">
					<div v-if="authMode === 'register' || authMode === 'forgot' || authMode === 'resend'" class="field">
            <label for="account-email">{{ t('auth.email') }}</label>
            <input id="account-email" v-model="accountForm.email" type="email" autocomplete="email" required />
            <span v-if="allowedDomains.length" class="hint">{{ t('auth.allowedEmailDomains', { domains: allowedDomains.join(', ') }) }}</span>
          </div>
          <div v-if="authMode === 'register'" class="field"><label for="display-name">{{ t('auth.displayName') }}</label><input id="display-name" v-model="accountForm.displayName" autocomplete="name" /></div>
          <div v-if="authMode === 'register' && app.publicSettings?.invitation_required" class="field"><label for="invitation-code">{{ t('auth.invitationCode') }}</label><input id="invitation-code" v-model="accountForm.invitationCode" autocomplete="one-time-code" required /></div>
          <div v-if="authMode === 'register' || authMode === 'reset'" class="field"><label for="new-password">{{ t('auth.password') }}</label><input id="new-password" v-model="accountForm.password" type="password" minlength="10" maxlength="72" autocomplete="new-password" required /><span class="hint">{{ t('auth.passwordRule') }}</span></div>
          <div v-if="authMode === 'register' || authMode === 'reset'" class="field"><label for="confirm-password">{{ t('auth.confirmPassword') }}</label><input id="confirm-password" v-model="accountForm.confirmPassword" type="password" minlength="10" maxlength="72" autocomplete="new-password" required /></div>
					<TurnstileWidget v-if="turnstileRequired && (authMode === 'register' || authMode === 'forgot' || authMode === 'resend')" :site-key="app.publicSettings!.turnstile_site_key" :reset-key="turnstileResetKey" @token="turnstileToken = $event" />
          <label v-if="app.publicSettings?.login_agreement_enabled && authMode === 'register'" class="agreement-check">
            <input v-model="agreementAccepted" type="checkbox" required />
            <span>{{ t('auth.agreementPrefix') }} <a v-for="document in app.publicSettings.legal_documents" :key="document.id" :href="`/legal/${document.slug}`" target="_blank">{{ document.name }}</a></span>
          </label>
					<button class="button auth-submit" type="submit" :disabled="auth.loading || (turnstileRequired && (authMode === 'register' || authMode === 'forgot' || authMode === 'resend') && !turnstileToken)">
						{{ authMode === 'register' ? t('auth.createAccount') : authMode === 'forgot' ? t('auth.sendResetEmail') : authMode === 'resend' ? t('auth.resendVerification') : t('auth.resetPassword') }}
          </button>
          <button class="button secondary auth-submit" type="button" @click="goTo('/login')">{{ t('auth.backToLogin') }}</button>
        </form>
      </section>

      <p class="auth-footer">&copy; {{ new Date().getFullYear() }} {{ app.siteName }}. {{ t('auth.rightsReserved') }}</p>
    </div>
  </main>
</template>
