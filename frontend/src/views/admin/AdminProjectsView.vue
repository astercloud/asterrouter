<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Boxes, Edit3, Plus, RefreshCw, Save, Search, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { createApplication, createProject, getApplications, getDepartments, getProjects, updateApplication, updateProject } from '@/api/control'
import type { Application, ApplicationRequest, Department, Project, ProjectRequest } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const projects = ref<Project[]>([])
const applications = ref<Application[]>([])
const departments = ref<Department[]>([])
const query = ref('')
const statusFilter = ref('')
const modal = ref<'project' | 'application' | ''>('')
const editingProject = ref<Project | null>(null)
const editingApplication = ref<Application | null>(null)

const projectForm = reactive<ProjectRequest>({
  name: '',
  description: '',
  cost_center: '',
  monthly_budget_cents: 50000,
  status: 'active'
})
const appForm = reactive<ApplicationRequest>({
  project_id: '',
  name: '',
  environment: 'dev',
  owner: '',
  status: 'active'
})

const appsByProject = computed(() => {
  const out = new Map<string, Application[]>()
  for (const app of applications.value) {
    out.set(app.project_id, [...(out.get(app.project_id) || []), app])
  }
  return out
})

const filteredProjects = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return projects.value.filter((project) => {
    if (statusFilter.value && project.status !== statusFilter.value) return false
    if (!keyword) return true
    return [project.name, project.description, project.cost_center].some((value) => value.toLowerCase().includes(keyword))
  })
})

const costCenterOptions = computed(() => {
  const values = new Set<string>()
  for (const department of departments.value) {
    if (department.status === 'active' && department.cost_center) {
      values.add(department.cost_center)
    }
  }
  for (const project of projects.value) {
    if (project.cost_center) {
      values.add(project.cost_center)
    }
  }
  return Array.from(values).sort()
})

const summary = computed(() => ({
  projects: projects.value.length,
  apps: applications.value.length,
  active: projects.value.filter((item) => item.status === 'active').length,
  archived: projects.value.filter((item) => item.status === 'archived').length
}))

const modalTitle = computed(() => {
  if (modal.value === 'project') {
    return editingProject.value ? t('projects.editProject') : t('projects.newProject')
  }
  if (modal.value === 'application') {
    return editingApplication.value ? t('projects.editApplication') : t('projects.newApplication')
  }
  return ''
})

function resetProjectForm() {
  Object.assign(projectForm, {
    name: '',
    description: '',
    cost_center: '',
    monthly_budget_cents: 50000,
    status: 'active'
  })
}

function resetAppForm(projectID = '') {
  Object.assign(appForm, {
    project_id: projectID || projects.value[0]?.id || '',
    name: '',
    environment: 'dev',
    owner: '',
    status: 'active'
  })
}

function openProjectModal() {
  editingProject.value = null
  resetProjectForm()
  modal.value = 'project'
}

function openProjectEdit(project: Project) {
  editingProject.value = project
  Object.assign(projectForm, {
    name: project.name,
    description: project.description,
    cost_center: project.cost_center,
    monthly_budget_cents: project.monthly_budget_cents,
    status: project.status
  })
  modal.value = 'project'
}

function openApplicationModal(projectID = '') {
  editingApplication.value = null
  resetAppForm(projectID)
  modal.value = 'application'
}

function openApplicationEdit(app: Application) {
  editingApplication.value = app
  Object.assign(appForm, {
    project_id: app.project_id,
    name: app.name,
    environment: app.environment,
    owner: app.owner,
    status: app.status
  })
  modal.value = 'application'
}

function closeModal() {
  modal.value = ''
  editingProject.value = null
  editingApplication.value = null
}

function formatCost(cents: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2
  }).format(cents / 100)
}

function formatPercent(value: number): string {
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value)}%`
}

function budgetStatusClass(status: string): string {
  if (status === 'ok' || status === 'unlimited') return 'status-success'
  if (status === 'exceeded') return 'status-danger'
  return 'status-warning'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [projectData, appData, departmentData] = await Promise.all([getProjects(), getApplications(), getDepartments()])
    projects.value = projectData
    applications.value = appData
    departments.value = departmentData
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function saveProject() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const project = editingProject.value
      ? await updateProject(editingProject.value.id, { ...projectForm })
      : await createProject({ ...projectForm })
    message.value = editingProject.value ? t('projects.updated') : t('projects.created')
    closeModal()
    await load()
    resetAppForm(project.id)
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function saveApplication() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    if (editingApplication.value) {
      await updateApplication(editingApplication.value.id, { ...appForm })
      message.value = t('projects.appUpdated')
    } else {
      await createApplication(appForm.project_id, { ...appForm })
      message.value = t('projects.appCreated')
    }
    closeModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.projects') }}</h1>
        <p>{{ t('projects.subtitle') }}</p>
      </div>
      <div class="row-actions">
        <button class="button secondary" type="button" @click="openApplicationModal()">
          <Boxes :size="17" />
          {{ t('projects.newApplication') }}
        </button>
        <button class="button" type="button" @click="openProjectModal">
          <Plus :size="17" />
          {{ t('projects.newProject') }}
        </button>
      </div>
    </section>

    <div class="crud-summary">
      <span><strong>{{ summary.projects }}</strong>{{ t('dashboard.projects') }}</span>
      <span><strong>{{ summary.apps }}</strong>{{ t('dashboard.apps') }}</span>
      <span><strong>{{ summary.active }}</strong>{{ t('dashboard.active') }}</span>
      <span><strong>{{ summary.archived }}</strong>{{ t('projects.archived') }}</span>
    </div>

    <section class="table-toolbar">
      <label class="search-box">
        <Search :size="17" />
        <input v-model="query" :placeholder="t('projects.searchPlaceholder')" />
      </label>
      <select v-model="statusFilter">
        <option value="">{{ t('providers.allStatuses') }}</option>
        <option value="active">active</option>
        <option value="archived">archived</option>
      </select>
      <button class="button secondary" type="button" :disabled="loading" @click="load">
        <RefreshCw :size="17" />
        {{ t('common.refresh') }}
      </button>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="panel table-panel content-fit">
      <div class="panel-body table-scroll">
        <table class="data-table crud-table">
          <thead>
            <tr>
              <th>{{ t('projects.name') }}</th>
              <th>{{ t('projects.costCenter') }}</th>
              <th>{{ t('projects.monthlyBudget') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('projects.applications') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="project in filteredProjects" :key="project.id">
              <td>
                <strong>{{ project.name }}</strong>
                <span>{{ project.description || '-' }}</span>
              </td>
              <td>{{ project.cost_center || '-' }}</td>
              <td>
                <strong>{{ project.monthly_budget_cents ? formatCost(project.monthly_budget_cents) : t('apiKeys.unlimited') }}</strong>
                <span>{{ t('projects.budgetUsed') }} {{ formatCost(project.current_month_cost_cents || 0) }}</span>
                <span>
                  {{ t('projects.budgetRemaining') }}
                  {{ project.monthly_budget_cents ? formatCost(project.budget_remaining_cents || 0) : t('apiKeys.unlimited') }}
                </span>
                <span class="pill" :class="budgetStatusClass(project.budget_status)">
                  {{ project.budget_status || 'unlimited' }}
                  <template v-if="project.monthly_budget_cents"> / {{ formatPercent(project.budget_used_percent || 0) }}</template>
                </span>
              </td>
              <td><span class="pill" :class="project.status === 'active' ? 'status-success' : 'status-warning'">{{ project.status }}</span></td>
              <td>
                <div class="chip-list">
                  <button v-for="app in appsByProject.get(project.id) || []" :key="app.id" class="pill" type="button" @click="openApplicationEdit(app)">
                    <Edit3 :size="12" />
                    {{ app.name }} / {{ app.environment }}
                  </button>
                </div>
              </td>
              <td>
                <button class="button secondary" type="button" @click="openProjectEdit(project)">
                  <Edit3 :size="15" />
                  {{ t('common.edit') }}
                </button>
                <button class="button secondary" type="button" @click="openApplicationModal(project.id)">
                  <Plus :size="15" />
                  {{ t('projects.newApplication') }}
                </button>
              </td>
            </tr>
            <tr v-if="!filteredProjects.length">
              <td colspan="6" class="empty-cell">{{ loading ? t('common.loading') : t('projects.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="modal" class="modal-backdrop" @click.self="closeModal">
      <section class="modal-card">
        <header class="modal-header">
          <div>
            <h2>{{ modalTitle }}</h2>
            <p>{{ modal === 'project' ? t('projects.projectModalSubtitle') : t('projects.appModalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" @click="closeModal"><X :size="19" /></button>
        </header>

        <div v-if="modal === 'project'" class="modal-body form-grid">
          <div class="field">
            <label>{{ t('projects.name') }}</label>
            <input v-model="projectForm.name" />
          </div>
          <div class="field">
            <label>{{ t('projects.costCenter') }}</label>
            <input v-model="projectForm.cost_center" list="project-cost-center-options" autocomplete="off" />
            <datalist id="project-cost-center-options">
              <option v-for="costCenter in costCenterOptions" :key="costCenter" :value="costCenter"></option>
            </datalist>
          </div>
          <div class="field form-span-2">
            <label>{{ t('projects.description') }}</label>
            <input v-model="projectForm.description" />
          </div>
          <div class="field">
            <label>{{ t('projects.monthlyBudget') }}</label>
            <input v-model.number="projectForm.monthly_budget_cents" type="number" min="0" step="100" />
          </div>
          <div class="field">
            <label>{{ t('providers.status') }}</label>
            <select v-model="projectForm.status">
              <option value="active">active</option>
              <option value="archived">archived</option>
            </select>
          </div>
        </div>

        <div v-else class="modal-body form-grid">
          <div class="field form-span-2">
            <label>{{ t('projects.project') }}</label>
            <select v-model="appForm.project_id">
              <option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('projects.appName') }}</label>
            <input v-model="appForm.name" />
          </div>
          <div class="field">
            <label>{{ t('projects.environment') }}</label>
            <select v-model="appForm.environment">
              <option value="dev">dev</option>
              <option value="staging">staging</option>
              <option value="prod">prod</option>
            </select>
          </div>
          <div class="field">
            <label>{{ t('projects.owner') }}</label>
            <input v-model="appForm.owner" />
          </div>
          <div class="field">
            <label>{{ t('providers.status') }}</label>
            <select v-model="appForm.status">
              <option value="active">active</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
        </div>

        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeModal">{{ t('common.cancel') }}</button>
          <button class="button" type="button" :disabled="saving" @click="modal === 'project' ? saveProject() : saveApplication()">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </section>
    </div>
  </main>
</template>
