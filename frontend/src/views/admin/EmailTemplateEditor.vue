<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Eye, RotateCcw, Save, Send } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { getEmailTemplate, getEmailTemplateCatalog, previewEmailTemplate, restoreEmailTemplate, testEmailTemplate, updateEmailTemplate } from '@/api/settings'
import type { EmailTemplateCatalog, SMTPTestConfig } from '@/types'

const props = defineProps<{ smtpConfig: SMTPTestConfig }>()
const { t, locale: appLocale } = useI18n()

const catalog = ref<EmailTemplateCatalog | null>(null)
const selectedEvent = ref('')
const selectedLocale = ref('')
const subject = ref('')
const html = ref('')
const customized = ref(false)
const placeholders = ref<string[]>([])
const recipient = ref('')
const preview = ref({ subject: '', html: '' })
const loadingCatalog = ref(false)
const loadingTemplate = ref(false)
const saving = ref(false)
const restoring = ref(false)
const previewing = ref(false)
const testing = ref(false)
const message = ref('')
const error = ref('')
let loadSequence = 0
let previewSequence = 0

const canEdit = computed(() => Boolean(selectedEvent.value && selectedLocale.value && subject.value.trim() && html.value.trim()))
const selectedEventInfo = computed(() => catalog.value?.events.find((item) => item.event === selectedEvent.value))
const activePlaceholders = computed(() => placeholders.value.length ? placeholders.value : selectedEventInfo.value?.placeholders || catalog.value?.placeholders || [])

function eventLabel(event: string): string {
	return t(`settings.emailTemplateEvents.${event}`)
}

function eventDescription(event: string): string {
	return t(`settings.emailTemplateEventHelp.${event}`)
}

function localeLabel(value: string): string {
	return value === 'zh-CN' ? t('settings.localeChinese') : t('settings.localeEnglish')
}

async function loadCatalog() {
	loadingCatalog.value = true
	error.value = ''
	try {
		catalog.value = await getEmailTemplateCatalog()
		const preferredLocale = catalog.value.locales.includes(appLocale.value as 'en-US' | 'zh-CN')
			? appLocale.value
			: catalog.value.locales[0] || ''
		selectedEvent.value = catalog.value.events[0]?.event || ''
		selectedLocale.value = preferredLocale
	} catch (err) {
		error.value = err instanceof Error ? err.message : t('common.failed')
	} finally {
		loadingCatalog.value = false
	}
}

async function loadTemplate() {
	if (!selectedEvent.value || !selectedLocale.value) return
	const sequence = ++loadSequence
	previewSequence++
	loadingTemplate.value = true
	error.value = ''
	message.value = ''
	try {
		const template = await getEmailTemplate(selectedEvent.value, selectedLocale.value)
		if (sequence !== loadSequence) return
		subject.value = template.subject
		html.value = template.html
		customized.value = template.customized
		placeholders.value = template.placeholders || []
		await refreshPreview()
	} catch (err) {
		if (sequence === loadSequence) error.value = err instanceof Error ? err.message : t('common.failed')
	} finally {
		if (sequence === loadSequence) loadingTemplate.value = false
	}
}

async function refreshPreview() {
	if (!subject.value.trim() || !html.value.trim()) return
	const sequence = ++previewSequence
	const previewSubject = subject.value
	const previewHTML = html.value
	previewing.value = true
	error.value = ''
	try {
		const result = await previewEmailTemplate(previewSubject, previewHTML)
		if (sequence === previewSequence) preview.value = result
	} catch (err) {
		if (sequence === previewSequence) error.value = err instanceof Error ? err.message : t('common.failed')
	} finally {
		if (sequence === previewSequence) previewing.value = false
	}
}

async function saveTemplate() {
	if (!canEdit.value) return
	saving.value = true
	error.value = ''
	message.value = ''
	try {
		const template = await updateEmailTemplate(selectedEvent.value, selectedLocale.value, subject.value, html.value)
		customized.value = template.customized
		placeholders.value = template.placeholders || []
		message.value = t('settings.emailTemplateSaved')
		await refreshPreview()
	} catch (err) {
		error.value = err instanceof Error ? err.message : t('common.failed')
	} finally {
		saving.value = false
	}
}

async function restoreOfficial() {
	if (!selectedEvent.value || !selectedLocale.value || !window.confirm(t('settings.emailTemplateRestoreConfirm'))) return
	restoring.value = true
	error.value = ''
	message.value = ''
	try {
		const template = await restoreEmailTemplate(selectedEvent.value, selectedLocale.value)
		subject.value = template.subject
		html.value = template.html
		customized.value = template.customized
		placeholders.value = template.placeholders || []
		message.value = t('settings.emailTemplateRestored')
		await refreshPreview()
	} catch (err) {
		error.value = err instanceof Error ? err.message : t('common.failed')
	} finally {
		restoring.value = false
	}
}

async function sendTest() {
	if (!recipient.value || !canEdit.value) return
	testing.value = true
	error.value = ''
	message.value = ''
	try {
		await testEmailTemplate(recipient.value, subject.value, html.value, props.smtpConfig)
		message.value = t('settings.emailTemplateTestSent')
	} catch (err) {
		error.value = err instanceof Error ? err.message : t('common.failed')
	} finally {
		testing.value = false
	}
}

watch([selectedEvent, selectedLocale], loadTemplate)
onMounted(loadCatalog)
</script>

<template>
  <section class="panel email-template-editor">
    <div class="panel-header split-header">
      <div>
        <h2>{{ t('settings.emailTemplates') }}</h2>
        <p>{{ t('settings.emailTemplatesHelp') }}</p>
      </div>
      <span v-if="customized" class="pill">{{ t('settings.emailTemplateCustomized') }}</span>
    </div>
    <div class="panel-body">
      <div v-if="message" class="notice success">{{ message }}</div>
      <div v-if="error" class="notice">{{ error }}</div>
      <div v-if="loadingCatalog" class="email-template-loading">{{ t('common.loading') }}</div>
      <template v-else-if="catalog?.events.length && catalog.locales.length">
        <div class="auth-credential-grid">
          <div class="field">
            <label for="email-template-event">{{ t('settings.emailTemplateEvent') }}</label>
            <select id="email-template-event" v-model="selectedEvent" :disabled="loadingTemplate || saving || restoring">
              <option v-for="item in catalog.events" :key="item.event" :value="item.event">{{ eventLabel(item.event) }}</option>
            </select>
          </div>
          <div class="field">
            <label for="email-template-locale">{{ t('settings.emailTemplateLocale') }}</label>
            <select id="email-template-locale" v-model="selectedLocale" :disabled="loadingTemplate || saving || restoring">
              <option v-for="item in catalog.locales" :key="item" :value="item">{{ localeLabel(item) }}</option>
            </select>
          </div>
        </div>

        <div v-if="selectedEvent" class="email-template-context">
          <strong>{{ eventLabel(selectedEvent) }}</strong>
          <span>{{ eventDescription(selectedEvent) }}</span>
        </div>

        <div v-if="loadingTemplate" class="email-template-loading">{{ t('common.loading') }}</div>
        <div v-else class="email-template-grid">
          <div class="email-template-form">
            <div class="field">
              <label for="email-template-subject">{{ t('settings.emailTemplateSubject') }}</label>
              <input id="email-template-subject" v-model="subject" autocomplete="off" />
            </div>
            <div class="field">
              <label for="email-template-html">{{ t('settings.emailTemplateHtml') }}</label>
              <textarea id="email-template-html" v-model="html" rows="18" class="code-input" />
            </div>
            <div class="email-template-placeholders">
              <strong>{{ t('settings.emailTemplatePlaceholders') }}</strong>
              <span>{{ t('settings.emailTemplatePlaceholdersHelp') }}</span>
              <div><code v-for="item in activePlaceholders" :key="item">{{ item }}</code></div>
            </div>
            <div class="email-template-actions">
              <button class="button secondary" type="button" :disabled="previewing || !canEdit" @click="refreshPreview"><Eye :size="16" />{{ previewing ? t('common.loading') : t('settings.emailTemplatePreview') }}</button>
              <button class="button secondary" type="button" :disabled="restoring || !customized" @click="restoreOfficial"><RotateCcw :size="16" />{{ restoring ? t('common.loading') : t('settings.emailTemplateRestore') }}</button>
              <button class="button" type="button" :disabled="saving || !canEdit" @click="saveTemplate"><Save :size="16" />{{ saving ? t('common.loading') : t('common.save') }}</button>
            </div>
            <div class="email-template-test">
              <div class="field">
                <label for="email-template-recipient">{{ t('settings.smtpTestRecipient') }}</label>
                <input id="email-template-recipient" v-model="recipient" type="email" autocomplete="off" placeholder="admin@example.com" />
              </div>
              <button class="button secondary" type="button" :disabled="testing || !recipient || !canEdit" @click="sendTest"><Send :size="16" />{{ testing ? t('common.loading') : t('settings.emailTemplateTest') }}</button>
            </div>
          </div>
          <div class="email-preview">
            <strong>{{ preview.subject || t('settings.emailTemplatePreviewEmpty') }}</strong>
            <iframe sandbox="" :srcdoc="preview.html" :title="t('settings.emailTemplatePreview')" />
            <span class="hint">{{ t('settings.emailTemplatePreviewSecurity') }}</span>
          </div>
        </div>
      </template>
      <div v-else class="notice">{{ t('settings.emailTemplateEmpty') }}</div>
    </div>
  </section>
</template>
