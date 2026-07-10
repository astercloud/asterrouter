import { apiClient } from './client'
import type { SystemApplyResult, SystemUpdateInfo } from '@/types'

export async function checkSystemUpdates(force = false): Promise<SystemUpdateInfo> {
  const response = await apiClient.get<SystemUpdateInfo>('/admin/system/check-updates', {
    params: { force }
  })
  return response.data
}

export async function performSystemUpdate(): Promise<SystemApplyResult> {
  const response = await apiClient.post<SystemApplyResult>('/admin/system/update')
  return response.data
}

export async function rollbackSystemUpdate(): Promise<SystemApplyResult> {
  const response = await apiClient.post<SystemApplyResult>('/admin/system/rollback')
  return response.data
}

export async function restartSystem(): Promise<SystemApplyResult> {
  const response = await apiClient.post<SystemApplyResult>('/admin/system/restart')
  return response.data
}
