import { apiClient } from './client'
import type {
  APIKeyCreateRequest,
  APIKeyCreateResponse,
  APIKeyRecord,
  APIKeyUpdateRequest,
  Application,
  ApplicationRequest,
  AlertEvent,
  AlertSummary,
  AuditLog,
  AuditLogSummary,
  CostAllocationReport,
  Department,
  DepartmentRequest,
  Dashboard,
  ExportJob,
  ExportJobKind,
  GatewayTrace,
  GatewayTraceSummary,
  ModelPricing,
  ModelPricingRequest,
  PortalWorkspace,
  Project,
  ProjectRequest,
  RecordListQuery,
  RoleBinding,
  RoleBindingRequest,
  ProviderAccount,
  ProviderAccountHealthCheck,
  ProviderAccountRequest,
  ProviderHealthCheck,
  ProviderConnection,
  ProviderRequest,
  RoutingGroup,
  RoutingGroupRequest,
  UsageReport,
  WorkspaceUser,
  WorkspaceUserRequest
} from '@/types'

export async function getDashboard(): Promise<Dashboard> {
  const response = await apiClient.get<Dashboard>('/admin/dashboard')
  return response.data
}

export async function getProviders(): Promise<ProviderConnection[]> {
  const response = await apiClient.get<ProviderConnection[]>('/admin/providers')
  return response.data
}

export async function getProviderHealthChecks(): Promise<ProviderHealthCheck[]> {
  const response = await apiClient.get<ProviderHealthCheck[]>('/admin/provider-health-checks')
  return response.data
}

export async function createProvider(payload: ProviderRequest): Promise<ProviderConnection> {
  const response = await apiClient.post<ProviderConnection>('/admin/providers', payload)
  return response.data
}

export async function updateProvider(id: string, payload: ProviderRequest): Promise<ProviderConnection> {
  const response = await apiClient.put<ProviderConnection>(`/admin/providers/${id}`, payload)
  return response.data
}

export async function checkProvider(id: string): Promise<ProviderHealthCheck> {
  const response = await apiClient.post<ProviderHealthCheck>(`/admin/providers/${id}/check`)
  return response.data
}

export async function getProjects(): Promise<Project[]> {
  const response = await apiClient.get<Project[]>('/admin/projects')
  return response.data
}

export async function createProject(payload: ProjectRequest): Promise<Project> {
  const response = await apiClient.post<Project>('/admin/projects', payload)
  return response.data
}

export async function updateProject(id: string, payload: ProjectRequest): Promise<Project> {
  const response = await apiClient.put<Project>(`/admin/projects/${id}`, payload)
  return response.data
}

export async function getDepartments(): Promise<Department[]> {
  const response = await apiClient.get<Department[]>('/admin/departments')
  return response.data
}

export async function createDepartment(payload: DepartmentRequest): Promise<Department> {
  const response = await apiClient.post<Department>('/admin/departments', payload)
  return response.data
}

export async function updateDepartment(id: string, payload: DepartmentRequest): Promise<Department> {
  const response = await apiClient.put<Department>(`/admin/departments/${id}`, payload)
  return response.data
}

export async function getApplications(): Promise<Application[]> {
  const response = await apiClient.get<Application[]>('/admin/applications')
  return response.data
}

export async function createApplication(projectID: string, payload: ApplicationRequest): Promise<Application> {
  const response = await apiClient.post<Application>(`/admin/projects/${projectID}/applications`, payload)
  return response.data
}

export async function updateApplication(id: string, payload: ApplicationRequest): Promise<Application> {
  const response = await apiClient.put<Application>(`/admin/applications/${id}`, payload)
  return response.data
}

export async function getWorkspaceUsers(): Promise<WorkspaceUser[]> {
  const response = await apiClient.get<WorkspaceUser[]>('/admin/users')
  return response.data
}

export async function createWorkspaceUser(payload: WorkspaceUserRequest): Promise<WorkspaceUser> {
  const response = await apiClient.post<WorkspaceUser>('/admin/users', payload)
  return response.data
}

export async function updateWorkspaceUser(id: string, payload: WorkspaceUserRequest): Promise<WorkspaceUser> {
  const response = await apiClient.put<WorkspaceUser>(`/admin/users/${id}`, payload)
  return response.data
}

export async function getRoleBindings(): Promise<RoleBinding[]> {
  const response = await apiClient.get<RoleBinding[]>('/admin/role-bindings')
  return response.data
}

export async function createRoleBinding(payload: RoleBindingRequest): Promise<RoleBinding> {
  const response = await apiClient.post<RoleBinding>('/admin/role-bindings', payload)
  return response.data
}

export async function deleteRoleBinding(id: string): Promise<void> {
  await apiClient.delete(`/admin/role-bindings/${id}`)
}

export async function getRoutingGroups(): Promise<RoutingGroup[]> {
  const response = await apiClient.get<RoutingGroup[]>('/admin/routing-groups')
  return response.data
}

export async function createRoutingGroup(payload: RoutingGroupRequest): Promise<RoutingGroup> {
  const response = await apiClient.post<RoutingGroup>('/admin/routing-groups', payload)
  return response.data
}

export async function updateRoutingGroup(id: string, payload: RoutingGroupRequest): Promise<RoutingGroup> {
  const response = await apiClient.put<RoutingGroup>(`/admin/routing-groups/${id}`, payload)
  return response.data
}

export async function getProviderAccounts(): Promise<ProviderAccount[]> {
  const response = await apiClient.get<ProviderAccount[]>('/admin/provider-accounts')
  return response.data
}

export async function getProviderAccountHealthChecks(): Promise<ProviderAccountHealthCheck[]> {
  const response = await apiClient.get<ProviderAccountHealthCheck[]>('/admin/provider-account-health-checks')
  return response.data
}

export async function createProviderAccount(payload: ProviderAccountRequest): Promise<ProviderAccount> {
  const response = await apiClient.post<ProviderAccount>('/admin/provider-accounts', payload)
  return response.data
}

export async function updateProviderAccount(id: string, payload: ProviderAccountRequest): Promise<ProviderAccount> {
  const response = await apiClient.put<ProviderAccount>(`/admin/provider-accounts/${id}`, payload)
  return response.data
}

export async function checkProviderAccount(id: string): Promise<ProviderAccountHealthCheck> {
  const response = await apiClient.post<ProviderAccountHealthCheck>(`/admin/provider-accounts/${id}/check`)
  return response.data
}

export async function getModelPricings(): Promise<ModelPricing[]> {
  const response = await apiClient.get<ModelPricing[]>('/admin/model-pricings')
  return response.data
}

export async function createModelPricing(payload: ModelPricingRequest): Promise<ModelPricing> {
  const response = await apiClient.post<ModelPricing>('/admin/model-pricings', payload)
  return response.data
}

export async function updateModelPricing(id: string, payload: ModelPricingRequest): Promise<ModelPricing> {
  const response = await apiClient.put<ModelPricing>(`/admin/model-pricings/${id}`, payload)
  return response.data
}

export async function getAPIKeys(): Promise<APIKeyRecord[]> {
  const response = await apiClient.get<APIKeyRecord[]>('/admin/api-keys')
  return response.data
}

export async function createAPIKey(payload: APIKeyCreateRequest): Promise<APIKeyCreateResponse> {
  const response = await apiClient.post<APIKeyCreateResponse>('/admin/api-keys', payload)
  return response.data
}

export async function updateAPIKey(id: string, payload: APIKeyUpdateRequest): Promise<APIKeyRecord> {
  const response = await apiClient.put<APIKeyRecord>(`/admin/api-keys/${id}`, payload)
  return response.data
}

export async function rotateAPIKey(id: string): Promise<APIKeyCreateResponse> {
  const response = await apiClient.post<APIKeyCreateResponse>(`/admin/api-keys/${id}/rotate`)
  return response.data
}

export async function disableAPIKey(id: string): Promise<void> {
  await apiClient.post(`/admin/api-keys/${id}/disable`)
}

export async function getAuditLogs(params?: RecordListQuery): Promise<AuditLog[]> {
  const response = await apiClient.get<AuditLog[]>('/admin/audit-logs', { params })
  return response.data
}

export async function getAuditLogSummary(params?: RecordListQuery): Promise<AuditLogSummary> {
  const response = await apiClient.get<AuditLogSummary>('/admin/audit-logs/summary', { params })
  return response.data
}

export async function getAlerts(params?: RecordListQuery): Promise<AlertEvent[]> {
  const response = await apiClient.get<AlertEvent[]>('/admin/alerts', { params })
  return response.data
}

export async function getAlertSummary(params?: RecordListQuery): Promise<AlertSummary> {
  const response = await apiClient.get<AlertSummary>('/admin/alerts/summary', { params })
  return response.data
}

export async function acknowledgeAlert(id: string): Promise<AlertEvent> {
  const response = await apiClient.post<AlertEvent>(`/admin/alerts/${id}/acknowledge`)
  return response.data
}

export async function resolveAlert(id: string): Promise<AlertEvent> {
  const response = await apiClient.post<AlertEvent>(`/admin/alerts/${id}/resolve`)
  return response.data
}

export async function exportAuditLogsCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/audit-logs/export', `audit-${Date.now()}.csv`, params)
}

export async function getUsageReport(params?: RecordListQuery): Promise<UsageReport> {
  const response = await apiClient.get<UsageReport>('/admin/usage', { params })
  return response.data
}

export async function exportUsageCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/usage/export', `usage-${Date.now()}.csv`, params)
}

export async function getCostAllocationReport(params?: RecordListQuery): Promise<CostAllocationReport> {
  const response = await apiClient.get<CostAllocationReport>('/admin/cost-allocation', { params })
  return response.data
}

export async function exportCostAllocationCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/cost-allocation/export', `cost-allocation-${Date.now()}.csv`, params)
}

export async function getGatewayTraces(params?: RecordListQuery): Promise<GatewayTrace[]> {
  const response = await apiClient.get<GatewayTrace[]>('/admin/gateway-traces', { params })
  return response.data
}

export async function getGatewayTraceSummary(params?: RecordListQuery): Promise<GatewayTraceSummary> {
  const response = await apiClient.get<GatewayTraceSummary>('/admin/gateway-traces/summary', { params })
  return response.data
}

export async function exportGatewayTracesCSV(params?: RecordListQuery): Promise<void> {
  await downloadCSV('/admin/gateway-traces/export', `gateway-traces-${Date.now()}.csv`, params)
}

export async function createExportJob(kind: ExportJobKind, params?: RecordListQuery): Promise<ExportJob> {
  const response = await apiClient.post<ExportJob>('/admin/export-jobs', null, { params: { ...params, kind } })
  return response.data
}

export async function getExportJobs(limit = 50): Promise<ExportJob[]> {
  const response = await apiClient.get<ExportJob[]>('/admin/export-jobs', { params: { limit } })
  return response.data
}

export async function getExportJob(id: string): Promise<ExportJob> {
  const response = await apiClient.get<ExportJob>(`/admin/export-jobs/${id}`)
  return response.data
}

export async function downloadExportJob(job: ExportJob): Promise<void> {
  await downloadCSV(`/admin/export-jobs/${job.id}/download`, job.filename)
}

export async function getPortalWorkspace(): Promise<PortalWorkspace> {
  const response = await apiClient.get<PortalWorkspace>('/portal/workspace')
  return response.data
}

async function downloadCSV(path: string, filename: string, params?: RecordListQuery): Promise<void> {
  const response = await apiClient.get<Blob>(path, { params, responseType: 'blob' })
  const blob = new Blob([response.data], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}
