<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Edit3, Plus, RefreshCw, Save, Search, ShieldCheck, Trash2, UserRound, X } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import {
  createRoleBinding,
  createWorkspaceUser,
  deleteRoleBinding,
	getDepartments,
  getRoleBindings,
  getWorkspaceUsers,
  updateWorkspaceUser
} from '@/api/control'
import type { Department, RoleBinding, RoleBindingRequest, WorkspaceUser, WorkspaceUserRequest } from '@/types'

type UserView = 'directory' | 'access'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const query = ref('')
const statusFilter = ref('')
const users = ref<WorkspaceUser[]>([])
const roleBindings = ref<RoleBinding[]>([])
const departments = ref<Department[]>([])
const userModalOpen = ref(false)
const bindingModalOpen = ref(false)
const editingUser = ref<WorkspaceUser | null>(null)
const activeView = ref<UserView>('directory')

const roleOptions = ['super_admin', 'platform_admin', 'key_manager', 'read_only_auditor', 'developer']
const resourceScopeOptions = ['dashboard', 'routing', 'providers', 'api_keys', 'usage', 'traces', 'alerts', 'identity', 'policies', 'audit', 'exports', 'plugins', 'settings', 'system']
const surfaceScopeOptions = ['personal', 'relay_operator', 'enterprise', 'platform', 'portal']

const userForm = reactive<WorkspaceUserRequest>({
  email: '',
  display_name: '',
  status: 'active',
  role: 'developer'
})

const bindingForm = reactive<RoleBindingRequest>({
  user_id: '',
  role: 'developer',
  scope_type: 'global',
  scope_id: ''
})

const filteredUsers = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  return users.value.filter((user) => {
    if (statusFilter.value && user.status !== statusFilter.value) return false
    if (!keyword) return true
    return [user.email, user.display_name, user.role, user.status].some((value) => value.toLowerCase().includes(keyword))
  })
})

const filteredBindings = computed(() => {
  const keyword = query.value.trim().toLowerCase()
  if (!keyword) return roleBindings.value
  return roleBindings.value.filter((binding) => [
    userLabel(binding.user_id),
    binding.role,
    binding.scope_type,
    binding.scope_id
  ].some((value) => value.toLowerCase().includes(keyword)))
})

const summary = computed(() => ({
  total: users.value.length,
  active: users.value.filter((item) => item.status === 'active').length,
  disabled: users.value.filter((item) => item.status === 'disabled').length,
  bindings: roleBindings.value.length
}))

function resetUserForm() {
  Object.assign(userForm, {
    email: '',
    display_name: '',
    status: 'active',
    role: 'developer'
  })
}

function openCreateUser() {
  editingUser.value = null
  resetUserForm()
  userModalOpen.value = true
}

function openEditUser(user: WorkspaceUser) {
  editingUser.value = user
  Object.assign(userForm, {
    email: user.email,
    display_name: user.display_name,
    status: user.status,
    role: user.role
  })
  userModalOpen.value = true
}

function closeUserModal() {
  userModalOpen.value = false
  editingUser.value = null
}

function openCreateBinding(user?: WorkspaceUser) {
  Object.assign(bindingForm, {
    user_id: user?.id || users.value[0]?.id || '',
    role: user?.role || 'developer',
    scope_type: 'global',
    scope_id: ''
  })
  bindingModalOpen.value = true
}

function closeBindingModal() {
  bindingModalOpen.value = false
}

function userLabel(userID: string): string {
  const user = users.value.find((item) => item.id === userID)
  return user ? `${user.display_name || user.email} · ${user.email}` : userID
}

function userFor(userID: string): WorkspaceUser | undefined {
  return users.value.find((item) => item.id === userID)
}

function userInitials(user: WorkspaceUser): string {
  const source = user.display_name || user.email || user.id
  return Array.from(source.trim()).slice(0, 2).join('').toUpperCase()
}

function bindingCount(userID: string): number {
  return roleBindings.value.filter((binding) => binding.user_id === userID).length
}

function roleLabel(role: string): string {
  const keys: Record<string, string> = {
    super_admin: 'users.roles.superAdmin',
    platform_admin: 'users.roles.platformAdmin',
    key_manager: 'users.roles.keyManager',
    read_only_auditor: 'users.roles.readOnlyAuditor',
    developer: 'users.roles.developer'
  }
  return keys[role] ? t(keys[role]) : role
}

function statusLabel(status: string): string {
  return status === 'active' ? t('users.activeStatus') : t('users.disabledStatus')
}

function scopeTypeLabel(scopeType: string): string {
  if (scopeType === 'global') return t('users.globalScope')
  if (scopeType === 'resource') return t('users.resourceScope')
  if (scopeType === 'surface') return t('users.surfaceScope')
  if (scopeType === 'department') return t('users.departmentScope')
  return scopeType
}

function formatDate(value: string): string {
  return value ? new Date(value).toLocaleString() : '-'
}

function scopeLabel(binding: RoleBinding): string {
  if (binding.scope_type === 'global') return t('users.globalScope')
  return binding.scope_id || binding.scope_type
}

function statusClass(status: string): string {
  if (status === 'active') return 'status-success'
  return 'status-danger'
}

function clearFilters() {
  query.value = ''
  statusFilter.value = ''
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [userData, bindingData, departmentData] = await Promise.all([getWorkspaceUsers(), getRoleBindings(), getDepartments()])
    users.value = userData
    roleBindings.value = bindingData
		departments.value = departmentData
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    loading.value = false
  }
}

async function saveUser() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    if (editingUser.value) {
      await updateWorkspaceUser(editingUser.value.id, userForm)
      message.value = t('users.updated')
    } else {
      await createWorkspaceUser(userForm)
      message.value = t('users.created')
    }
    closeUserModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function saveBinding() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    await createRoleBinding({
      ...bindingForm,
      scope_id: bindingForm.scope_type === 'global' ? '' : bindingForm.scope_id
    })
    message.value = t('users.bindingCreated')
    closeBindingModal()
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  } finally {
    saving.value = false
  }
}

async function revokeBinding(binding: RoleBinding) {
  if (!window.confirm(t('users.revokeConfirm'))) return
  error.value = ''
  message.value = ''
  try {
    await deleteRoleBinding(binding.id)
    message.value = t('users.bindingRevoked')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.failed')
  }
}

onMounted(load)
</script>

<template>
  <main class="content crud-page user-management-page">
    <section class="page-header">
      <div>
        <h1>{{ t('admin.users') }}</h1>
        <p>{{ t('users.subtitle') }}</p>
      </div>
      <div class="row-actions user-page-actions">
        <button class="button secondary" type="button" :disabled="loading" @click="load">
          <RefreshCw :size="17" />
          {{ t('common.refresh') }}
        </button>
        <button class="button" type="button" @click="openCreateUser">
          <Plus :size="17" />
          {{ t('users.newUser') }}
        </button>
      </div>
    </section>

    <div v-if="message" class="notice success">{{ message }}</div>
    <div v-if="error" class="notice">{{ error }}</div>

    <section class="user-summary-band" :aria-label="t('users.overviewLabel')">
      <div><span>{{ t('users.total') }}</span><strong>{{ summary.total }}</strong></div>
      <div><span>{{ t('users.active') }}</span><strong class="summary-good">{{ summary.active }}</strong></div>
      <div><span>{{ t('users.disabled') }}</span><strong :class="{ 'summary-warning': summary.disabled > 0 }">{{ summary.disabled }}</strong></div>
      <div><span>{{ t('users.bindings') }}</span><strong>{{ summary.bindings }}</strong></div>
    </section>

    <nav class="user-primary-tabs" :aria-label="t('users.tabsLabel')">
      <button
        class="user-primary-tab"
        :class="{ active: activeView === 'directory' }"
        type="button"
        data-view="directory"
        :aria-current="activeView === 'directory' ? 'page' : undefined"
        @click="activeView = 'directory'"
      >
        {{ t('users.directoryTab') }}
        <span>{{ users.length }}</span>
      </button>
      <button
        class="user-primary-tab"
        :class="{ active: activeView === 'access' }"
        type="button"
        data-view="access"
        :aria-current="activeView === 'access' ? 'page' : undefined"
        @click="activeView = 'access'"
      >
        {{ t('users.accessTab') }}
        <span>{{ roleBindings.length }}</span>
      </button>
    </nav>

    <section v-if="activeView === 'directory'" class="panel user-directory-panel" data-section="user-directory">
      <header class="panel-header split-header user-panel-header">
        <div>
          <h2>{{ t('users.workspaceUsers') }}</h2>
          <p>{{ t('users.workspaceUsersSubtitle') }}</p>
        </div>
        <span class="user-result-count">{{ t('users.resultCount', { count: filteredUsers.length }) }}</span>
      </header>

      <div class="user-toolbar">
        <label class="search-box">
          <Search :size="17" />
          <input v-model="query" :aria-label="t('common.search')" :placeholder="t('users.searchPlaceholder')" />
        </label>
        <select v-model="statusFilter" :aria-label="t('providers.status')">
          <option value="">{{ t('providers.allStatuses') }}</option>
          <option value="active">{{ t('users.activeStatus') }}</option>
          <option value="disabled">{{ t('users.disabledStatus') }}</option>
        </select>
      </div>

      <div class="panel-body table-scroll">
        <table class="data-table crud-table user-directory-table">
          <thead>
            <tr>
              <th>{{ t('users.user') }}</th>
              <th>{{ t('users.defaultRole') }}</th>
              <th>{{ t('users.bindings') }}</th>
              <th>{{ t('providers.status') }}</th>
              <th>{{ t('users.createdAt') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="user in filteredUsers" :key="user.id">
              <td>
                <div class="user-identity">
                  <span class="user-avatar" aria-hidden="true">{{ userInitials(user) }}</span>
                  <span class="user-identity-copy">
                    <strong>{{ user.display_name || user.email }}</strong>
                    <small>{{ user.email }}</small>
                  </span>
                </div>
              </td>
              <td><span class="pill role-pill">{{ roleLabel(user.role) }}</span></td>
              <td><strong>{{ bindingCount(user.id) }}</strong><span>{{ t('users.bindingCountHelp') }}</span></td>
              <td><span class="pill" :class="statusClass(user.status)">{{ statusLabel(user.status) }}</span></td>
              <td>{{ formatDate(user.created_at) }}</td>
              <td>
                <div class="row-actions compact-actions">
                  <button class="icon-button" type="button" :title="t('common.edit')" :aria-label="t('common.edit')" @click="openEditUser(user)">
                    <Edit3 :size="16" />
                  </button>
                  <button class="icon-button" type="button" :title="t('users.grantRole')" :aria-label="t('users.grantRole')" @click="openCreateBinding(user)">
                    <ShieldCheck :size="16" />
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!filteredUsers.length">
              <td colspan="6" class="empty-cell user-empty-cell">
                <div class="user-empty-state">
                  <UserRound :size="22" />
                  <strong>{{ loading ? t('common.loading') : t('users.emptyTitle') }}</strong>
                  <p v-if="!loading">{{ t('users.emptyHelp') }}</p>
                  <button v-if="!loading && (query || statusFilter)" class="button secondary tiny-button" type="button" @click="clearFilters">
                    {{ t('users.clearFilters') }}
                  </button>
                  <button v-else-if="!loading" class="button secondary tiny-button" type="button" @click="openCreateUser">
                    <Plus :size="15" />
                    {{ t('users.newUser') }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-else class="panel user-directory-panel" data-section="user-access">
      <header class="panel-header split-header user-panel-header">
        <div>
          <h2>{{ t('users.roleBindings') }}</h2>
          <p>{{ t('users.roleBindingsSubtitle') }}</p>
        </div>
        <button class="button secondary tiny-button" type="button" :disabled="!users.length" @click="openCreateBinding()">
          <ShieldCheck :size="16" />
          {{ t('users.grantRole') }}
        </button>
      </header>

      <div class="user-toolbar">
        <label class="search-box">
          <Search :size="17" />
          <input v-model="query" :aria-label="t('common.search')" :placeholder="t('users.accessSearchPlaceholder')" />
        </label>
        <span class="user-result-count">{{ t('users.resultCount', { count: filteredBindings.length }) }}</span>
      </div>

      <div class="panel-body table-scroll">
        <table class="data-table crud-table user-access-table">
          <thead>
            <tr>
              <th>{{ t('users.user') }}</th>
              <th>{{ t('users.role') }}</th>
              <th>{{ t('users.scope') }}</th>
              <th>{{ t('users.createdAt') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="binding in filteredBindings" :key="binding.id">
              <td>
                <strong>{{ userFor(binding.user_id)?.display_name || userFor(binding.user_id)?.email || binding.user_id }}</strong>
                <span>{{ userFor(binding.user_id)?.email || binding.user_id }}</span>
              </td>
              <td><span class="pill role-pill">{{ roleLabel(binding.role) }}</span></td>
              <td>
                <strong>{{ scopeTypeLabel(binding.scope_type) }}</strong>
                <span>{{ scopeLabel(binding) }}</span>
              </td>
              <td>{{ formatDate(binding.created_at) }}</td>
              <td>
                <button class="icon-button danger-action" type="button" :title="t('users.revoke')" :aria-label="t('users.revoke')" @click="revokeBinding(binding)">
                  <Trash2 :size="16" />
                </button>
              </td>
            </tr>
            <tr v-if="!filteredBindings.length">
              <td colspan="5" class="empty-cell user-empty-cell">
                <div class="user-empty-state">
                  <ShieldCheck :size="22" />
                  <strong>{{ loading ? t('common.loading') : t('users.noBindingsTitle') }}</strong>
                  <p v-if="!loading">{{ t('users.noBindingsHelp') }}</p>
                  <button v-if="!loading && query" class="button secondary tiny-button" type="button" @click="clearFilters">
                    {{ t('users.clearFilters') }}
                  </button>
                  <button v-else-if="!loading" class="button secondary tiny-button" type="button" :disabled="!users.length" @click="openCreateBinding()">
                    <ShieldCheck :size="15" />
                    {{ t('users.grantRole') }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="userModalOpen" class="modal-backdrop" @click.self="closeUserModal">
      <form class="modal-card user-modal" role="dialog" aria-modal="true" aria-labelledby="user-modal-title" @submit.prevent="saveUser">
        <header class="modal-header">
          <div>
            <h2 id="user-modal-title">{{ editingUser ? t('users.editUser') : t('users.newUser') }}</h2>
            <p>{{ t('users.userModalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeUserModal">
            <X :size="18" />
          </button>
        </header>
        <div class="modal-body form-grid">
          <div class="field">
            <label for="workspace-user-email">{{ t('users.email') }}</label>
            <input id="workspace-user-email" v-model="userForm.email" type="email" required autocomplete="off" />
          </div>
          <div class="field">
            <label for="workspace-user-name">{{ t('users.displayName') }}</label>
            <input id="workspace-user-name" v-model="userForm.display_name" autocomplete="off" />
          </div>
          <div class="field">
            <label for="workspace-user-role">{{ t('users.defaultRole') }}</label>
            <select id="workspace-user-role" v-model="userForm.role">
              <option v-for="role in roleOptions" :key="role" :value="role">{{ roleLabel(role) }}</option>
            </select>
          </div>
          <div class="field">
            <label for="workspace-user-status">{{ t('providers.status') }}</label>
            <select id="workspace-user-status" v-model="userForm.status">
              <option value="active">{{ t('users.activeStatus') }}</option>
              <option value="disabled">{{ t('users.disabledStatus') }}</option>
            </select>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeUserModal">{{ t('common.cancel') }}</button>
          <button class="button" type="submit" :disabled="saving">
            <Save :size="17" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </footer>
      </form>
    </div>

    <div v-if="bindingModalOpen" class="modal-backdrop" @click.self="closeBindingModal">
      <form class="modal-card user-modal" role="dialog" aria-modal="true" aria-labelledby="binding-modal-title" @submit.prevent="saveBinding">
        <header class="modal-header">
          <div>
            <h2 id="binding-modal-title">{{ t('users.grantRole') }}</h2>
            <p>{{ t('users.bindingModalSubtitle') }}</p>
          </div>
          <button class="icon-button" type="button" :aria-label="t('common.close')" @click="closeBindingModal">
            <X :size="18" />
          </button>
        </header>
        <div class="modal-body form-grid">
          <div class="field form-span-2">
            <label for="binding-user">{{ t('users.user') }}</label>
            <select id="binding-user" v-model="bindingForm.user_id" required>
              <option v-for="user in users" :key="user.id" :value="user.id">{{ user.display_name || user.email }} · {{ user.email }}</option>
            </select>
          </div>
          <div class="field">
            <label for="binding-role">{{ t('users.role') }}</label>
            <select id="binding-role" v-model="bindingForm.role">
              <option v-for="role in roleOptions" :key="role" :value="role">{{ roleLabel(role) }}</option>
            </select>
          </div>
          <div class="field">
            <label for="binding-scope">{{ t('users.scope') }}</label>
            <select id="binding-scope" v-model="bindingForm.scope_type" @change="bindingForm.scope_id = ''">
              <option value="global">{{ t('users.globalScope') }}</option>
              <option value="resource">{{ t('users.resourceScope') }}</option>
              <option value="surface">{{ t('users.surfaceScope') }}</option>
              <option value="department">{{ t('users.departmentScope') }}</option>
            </select>
          </div>
          <div v-if="bindingForm.scope_type !== 'global'" class="field form-span-2">
            <label for="binding-target">{{ t('users.scopeTarget') }}</label>
            <select id="binding-target" v-model="bindingForm.scope_id" required>
              <option value="" disabled>{{ t('users.selectScopeTarget') }}</option>
              <template v-if="bindingForm.scope_type === 'department'">
                <option v-for="department in departments" :key="department.id" :value="department.id">{{ department.name }}</option>
              </template>
              <template v-else>
                <option v-for="scope in bindingForm.scope_type === 'resource' ? resourceScopeOptions : surfaceScopeOptions" :key="scope" :value="scope">{{ scope }}</option>
              </template>
            </select>
          </div>
        </div>
        <footer class="modal-footer">
          <button class="button secondary" type="button" @click="closeBindingModal">{{ t('common.cancel') }}</button>
          <button class="button" type="submit" :disabled="saving">
            <UserRound :size="17" />
            {{ saving ? t('common.saving') : t('users.grantRole') }}
          </button>
        </footer>
      </form>
    </div>
  </main>
</template>

<style scoped>
.user-management-page .page-header {
  margin-bottom: 0;
}

.user-page-actions {
  flex-wrap: wrap;
}

.user-summary-band {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--panel-bg);
  box-shadow: var(--shadow-sm);
}

.user-summary-band > div {
  display: grid;
  min-height: 82px;
  align-content: center;
  gap: 5px;
  padding: 14px 18px;
  border-right: 1px solid var(--border);
}

.user-summary-band > div:last-child {
  border-right: 0;
}

.user-summary-band span {
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 650;
}

.user-summary-band strong {
  color: var(--text);
  font-size: 23px;
  line-height: 1.1;
}

.user-summary-band .summary-good {
  color: var(--success);
}

.user-summary-band .summary-warning {
  color: var(--warning);
}

.user-primary-tabs {
  display: flex;
  min-height: 42px;
  gap: 22px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  scrollbar-width: none;
}

.user-primary-tabs::-webkit-scrollbar {
  display: none;
}

.user-primary-tab {
  position: relative;
  display: inline-flex;
  min-height: 42px;
  flex: 0 0 auto;
  align-items: center;
  gap: 7px;
  padding: 0 2px;
  border: 0;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 13px;
  font-weight: 650;
}

.user-primary-tab > span {
  min-width: 20px;
  padding: 1px 6px;
  border-radius: var(--radius-pill);
  background: var(--surface-subtle);
  color: var(--text-muted);
  font-size: 10px;
  text-align: center;
}

.user-primary-tab::after {
  position: absolute;
  right: 0;
  bottom: -1px;
  left: 0;
  height: 2px;
  background: transparent;
  content: "";
}

.user-primary-tab:hover,
.user-primary-tab.active {
  color: var(--text);
}

.user-primary-tab.active::after {
  background: var(--primary-500);
}

.user-primary-tab:focus-visible {
  border-radius: var(--radius-sm);
  outline: 3px solid var(--focus-ring);
  outline-offset: -3px;
}

.user-directory-panel {
  min-height: 0;
  border-radius: var(--radius-sm);
}

.user-panel-header {
  min-height: 68px;
}

.user-panel-header > div {
  min-width: 0;
}

.user-panel-header p {
  margin: 3px 0 0;
  color: var(--text-muted);
  font-size: 12px;
}

.user-result-count {
  flex: 0 0 auto;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 650;
}

.user-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--surface-subtle);
}

.user-toolbar .search-box {
  min-width: 0;
  min-height: 38px;
}

.user-toolbar select {
  min-width: 150px;
  min-height: 38px;
  padding: 0 10px;
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-secondary);
}

.user-identity {
  display: flex;
  min-width: 220px;
  align-items: center;
  gap: 10px;
}

.user-avatar {
  display: inline-grid;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--primary-500) 28%, var(--border));
  border-radius: 50%;
  background: color-mix(in srgb, var(--primary-500) 10%, var(--surface));
  color: var(--primary-700);
  font-size: 11px;
  font-weight: 750;
}

:global(:root[data-theme="dark"]) .user-avatar {
  color: var(--primary-300);
}

.user-identity-copy {
  display: grid;
  min-width: 0;
  gap: 2px;
}

.user-identity-copy strong,
.user-identity-copy small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-identity-copy small {
  color: var(--text-muted);
  font-size: 11px;
}

.role-pill {
  color: var(--text-secondary);
}

.compact-actions {
  flex-wrap: nowrap;
}

.compact-actions .icon-button,
.user-access-table .icon-button {
  width: 32px;
  height: 32px;
}

.danger-action {
  border-color: color-mix(in srgb, var(--danger) 24%, var(--border));
  color: var(--danger);
}

.danger-action:hover {
  background: var(--danger-bg);
}

.user-empty-cell {
  height: 260px;
}

.user-empty-state {
  display: grid;
  justify-items: center;
  color: var(--text-muted);
}

.user-empty-state strong {
  margin-top: 10px;
  color: var(--text);
  font-size: 13px;
}

.user-empty-state p {
  max-width: 420px;
  margin: 4px 0 14px;
  font-size: 12px;
}

.user-modal {
  border-radius: var(--radius-sm);
}

@media (max-width: 760px) {
  .user-summary-band {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .user-summary-band > div:nth-child(2) {
    border-right: 0;
  }

  .user-summary-band > div:nth-child(-n + 2) {
    border-bottom: 1px solid var(--border);
  }

  .user-panel-header {
    display: grid;
    padding-top: 14px;
    padding-bottom: 14px;
  }

  .user-panel-header .button {
    width: 100%;
  }

  .user-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .user-toolbar select {
    width: 100%;
  }

  .user-toolbar .search-box {
    width: 100%;
    flex: 0 0 auto;
  }
}

@media (max-width: 520px) {
  .user-page-actions {
    width: 100%;
  }

  .user-page-actions .button {
    flex: 1;
  }

  .user-summary-band > div {
    min-height: 72px;
    padding: 12px 14px;
  }

  .user-summary-band strong {
    font-size: 21px;
  }

  .user-directory-table th:nth-child(3),
  .user-directory-table td:nth-child(3),
  .user-directory-table th:nth-child(5),
  .user-directory-table td:nth-child(5) {
    display: none;
  }

  .user-directory-table {
    min-width: 620px;
  }

  .user-access-table {
    min-width: 680px;
  }
}
</style>
