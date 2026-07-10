export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export interface PublicSettings {
  site_name: string
  site_subtitle: string
  public_base_url: string
  api_base_url: string
  gateway_base_path: string
  profile: string
  setup_completed: boolean
  default_locale: string
  enabled_locales: string[]
  oidc_enabled: boolean
  oidc_provider_name: string
  service_center_mode: string
  version: string
  server_timezone: string
  server_utc_offset: string
  storage_mode: string
}

export interface AuthUser {
  username: string
  role: string
}

export interface LoginResult {
  access_token: string
  token_type: string
  expires_at: string
  user: AuthUser
}

export interface AdminSettings extends PublicSettings {
  oidc_issuer_url: string
  oidc_client_id: string
  data_retention_days: number
  prompt_logging_mode: string
  update_channel: string
}

export interface LocaleInfo {
  code: string
  name: string
  native: string
}

export interface ProviderConnection {
  id: string
  name: string
  type: string
  base_url: string
  status: string
  models: string[]
  priority: number
  secret_configured: boolean
  secret_hint: string
  created_at: string
  updated_at: string
}

export interface ProviderRequest {
  name: string
  type: string
  base_url: string
  status: string
  models: string[]
  priority: number
  api_key: string
}

export interface ProviderHealthCheck {
  id: string
  provider_id: string
  status: string
  latency_ms: number
  message: string
  models: string[]
  checked_at: string
}

export interface Project {
  id: string
  name: string
  description: string
  cost_center: string
  monthly_budget_cents: number
  status: string
  created_at: string
  updated_at: string
}

export interface ProjectRequest {
  name: string
  description: string
  cost_center: string
  monthly_budget_cents: number
  status: string
}

export interface Application {
  id: string
  project_id: string
  name: string
  environment: string
  owner: string
  status: string
  created_at: string
  updated_at: string
}

export interface ApplicationRequest {
  project_id: string
  name: string
  environment: string
  owner: string
  status: string
}

export interface RoutingGroup {
  id: string
  name: string
  description: string
  platform: string
  rate_multiplier: number
  status: string
  sort_order: number
  account_count: number
  active_account_count: number
  created_at: string
  updated_at: string
}

export interface RoutingGroupRequest {
  name: string
  description: string
  platform: string
  rate_multiplier: number
  status: string
  sort_order: number
}

export interface ProviderAccount {
  id: string
  provider_id: string
  name: string
  platform: string
  auth_type: string
  status: string
  schedulable: boolean
  priority: number
  concurrency: number
  rate_multiplier: number
  models: string[]
  group_ids: string[]
  secret_configured: boolean
  secret_hint: string
  error_message: string
  last_used_at?: string
  expires_at?: string
  created_at: string
  updated_at: string
}

export interface ProviderAccountRequest {
  provider_id: string
  name: string
  platform: string
  auth_type: string
  status: string
  schedulable: boolean
  priority: number
  concurrency: number
  rate_multiplier: number
  models: string[]
  group_ids: string[]
  secret: string
  expires_at: string
}

export interface ProviderAccountHealthCheck {
  id: string
  account_id: string
  provider_id: string
  status: string
  latency_ms: number
  message: string
  models: string[]
  checked_at: string
}

export interface APIKeyRecord {
  id: string
  project_id: string
  application_id: string
  name: string
  fingerprint: string
  prefix: string
  status: string
  model_allowlist: string[]
  qps_limit: number
  monthly_token_limit: number
  expires_at?: string
  last_used_at?: string
  created_at: string
  updated_at: string
}

export interface APIKeyCreateRequest {
  project_id: string
  application_id: string
  name: string
  model_allowlist: string[]
  qps_limit: number
  monthly_token_limit: number
  expires_at: string
}

export interface APIKeyUpdateRequest {
  name: string
  model_allowlist: string[]
  qps_limit: number
  monthly_token_limit: number
  expires_at: string
  status: string
}

export interface APIKeyCreateResponse {
  record: APIKeyRecord
  key: string
}

export interface AuditLog {
  id: string
  actor: string
  action: string
  resource_type: string
  resource_id: string
  summary: string
  created_at: string
}

export interface Dashboard {
  provider_count: number
  active_provider_count: number
  project_count: number
  application_count: number
  api_key_count: number
  active_api_key_count: number
  models: string[]
  recent_audit: AuditLog[]
}

export interface PortalWorkspace {
  projects: Project[]
  applications: Application[]
  api_keys: APIKeyRecord[]
  models: string[]
  gateway_path: string
}

export interface SystemUpdateAsset {
  name: string
  url: string
  os: string
  arch: string
  sha256: string
  size: number
}

export interface SystemReleaseInfo {
  version: string
  name: string
  notes: string
  published_at: string
  html_url: string
  asset?: SystemUpdateAsset
  assets?: SystemUpdateAsset[]
}

export interface SystemUpdateInfo {
  current_version: string
  latest_version: string
  has_update: boolean
  release_info?: SystemReleaseInfo
  cached: boolean
  warning?: string
  build_type: string
  update_supported: boolean
  manifest_configured: boolean
  restart_supported: boolean
  channel: string
  platform: string
}

export interface SystemApplyResult {
  message: string
  operation_id: string
  need_restart: boolean
  already_up_to_date: boolean
  current_version: string
  latest_version: string
  manual_action?: string
}

export interface Plugin {
  id: string
  plugin_id: string
  name: string
  description: string
  category: string
  type: string
  tier: string
  version: string
  vendor: string
  status: string
  entitlement_status: string
  surfaces: string[]
  entry_point: string
  configurable: boolean
  created_at: string
  updated_at: string
}

export interface PluginSummary {
  total: number
  enabled: number
  free: number
  paid_locked: number
  configurable: number
}

export interface PluginCatalog {
  summary: PluginSummary
  plugins: Plugin[]
}

export interface UsageRecord {
  id: string
  project_id: string
  application_id: string
  api_key_id: string
  api_fingerprint: string
  model: string
  provider_id: string
  provider_account_id: string
  status: string
  error_type: string
  latency_ms: number
  input_tokens: number
  output_tokens: number
  cost_cents: number
  created_at: string
}

export interface UsageModelSummary {
  model: string
  requests: number
  errors: number
  tokens: number
  cost_cents: number
  avg_latency_ms: number
}

export interface UsageReport {
  total_requests: number
  error_requests: number
  total_tokens: number
  total_cost_cents: number
  avg_latency_ms: number
  by_model: UsageModelSummary[]
  recent: UsageRecord[]
}

export interface RecordListQuery {
  limit?: number
  offset?: number
  q?: string
  model?: string
  status?: string
  project_id?: string
  application_id?: string
  action?: string
  resource_type?: string
  from?: string
  to?: string
}

export interface GatewayTraceSummary {
  total: number
  routed: number
  errors: number
  tokens: number
  avg_latency_ms: number
}

export interface GatewayTrace {
  id: string
  project_id: string
  application_id: string
  api_key_id: string
  api_fingerprint: string
  model: string
  stream: boolean
  message_count: number
  provider_id: string
  provider_account_id: string
  route_source: string
  route_reason: string
  status: string
  http_status: number
  error_type: string
  latency_ms: number
  input_tokens: number
  output_tokens: number
  request_summary: string
  response_summary: string
  created_at: string
}

export interface AuditLogSummary {
  total: number
  actors: number
  resources: number
  actions: number
}

export type ExportJobKind = 'usage' | 'gateway_traces' | 'audit_logs'

export interface ExportJob {
  id: string
  kind: ExportJobKind
  status: string
  filename: string
  content_type: string
  row_count: number
  size_bytes: number
  error: string
  parameters: Record<string, string>
  created_at: string
  updated_at: string
  expires_at: string
}
