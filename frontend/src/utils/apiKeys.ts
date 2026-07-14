import type { APIKeyRecord } from '@/types'

export type APIKeyLifecycleStatus = 'active' | 'retiring' | 'retired' | 'disabled'

const lifecycleStatuses = new Set<APIKeyLifecycleStatus>(['active', 'retiring', 'retired', 'disabled'])

export function apiKeyLifecycleStatus(key: APIKeyRecord, now = Date.now()): APIKeyLifecycleStatus {
  if (lifecycleStatuses.has(key.lifecycle_status as APIKeyLifecycleStatus)) {
    return key.lifecycle_status as APIKeyLifecycleStatus
  }
  if (key.replaced_by_key_id) {
    const graceExpiresAt = key.rotation_grace_expires_at ? Date.parse(key.rotation_grace_expires_at) : Number.NaN
    return Number.isFinite(graceExpiresAt) && graceExpiresAt > now ? 'retiring' : 'retired'
  }
  return key.status === 'active' ? 'active' : 'disabled'
}

export function apiKeyLifecycleLabelKey(key: APIKeyRecord): string {
  return `apiKeys.lifecycle.${apiKeyLifecycleStatus(key)}`
}

export function apiKeyLifecycleClass(key: APIKeyRecord): string {
  const status = apiKeyLifecycleStatus(key)
  if (status === 'active') return 'status-success'
  if (status === 'retiring') return 'status-warning'
  return 'status-danger'
}

export function canRotateAPIKey(key: APIKeyRecord): boolean {
  return apiKeyLifecycleStatus(key) === 'active' && !key.replaced_by_key_id
}

export function canDisableAPIKey(key: APIKeyRecord): boolean {
  const status = apiKeyLifecycleStatus(key)
  return status === 'active' || status === 'retiring'
}
