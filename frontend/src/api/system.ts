import { apiClient } from './client'
import { listOrEmpty } from './normalizers'
import type { S3BackupObject, SystemApplyResult, SystemArchiveInfo, SystemRestoreResult, SystemUpdateInfo } from '@/types'

export async function checkSystemUpdates(force = false): Promise<SystemUpdateInfo> {
  const response = await apiClient.get<SystemUpdateInfo>('/console/system/check-updates', {
    params: { force }
  })
  return response.data
}

export async function performSystemUpdate(): Promise<SystemApplyResult> {
  const response = await apiClient.post<SystemApplyResult>('/console/system/update')
  return response.data
}

export async function rollbackSystemUpdate(): Promise<SystemApplyResult> {
  const response = await apiClient.post<SystemApplyResult>('/console/system/rollback')
  return response.data
}

export async function restartSystem(): Promise<SystemApplyResult> {
  const response = await apiClient.post<SystemApplyResult>('/console/system/restart')
  return response.data
}

export async function listSystemBackups(): Promise<SystemArchiveInfo[]> {
  const response = await apiClient.get<SystemArchiveInfo[] | null>('/console/system/backups')
  return listOrEmpty(response.data)
}

export async function createSystemBackup(): Promise<SystemArchiveInfo> {
  const response = await apiClient.post<SystemArchiveInfo>('/console/system/backups')
  return response.data
}

export async function testBackupS3(): Promise<void> {
  await apiClient.post('/console/system/backups/s3/test')
}

export async function listS3Backups(): Promise<S3BackupObject[]> {
  return listOrEmpty((await apiClient.get<S3BackupObject[] | null>('/console/system/backups/s3')).data)
}

export async function restoreS3Backup(key: string): Promise<SystemRestoreResult> {
  return (await apiClient.post<SystemRestoreResult>('/console/system/backups/s3/restore', { key, confirm: true })).data
}

export async function downloadS3Backup(backup: S3BackupObject): Promise<void> {
  const path = `/console/system/backups/s3/download?key=${encodeURIComponent(backup.key)}`
  await downloadArchive(path, `${backup.id}.tar.gz`)
}

export async function restoreSystemBackup(backupID: string): Promise<SystemRestoreResult> {
  const response = await apiClient.post<SystemRestoreResult>('/console/system/backups/restore', {
    backup_id: backupID,
    confirm: true
  })
  return response.data
}

export async function downloadSystemBackup(backup: SystemArchiveInfo): Promise<void> {
  await downloadArchive(`/console/system/backups/${encodeURIComponent(backup.id)}/download`, backup.path)
}

export async function createDiagnosticBundle(): Promise<SystemArchiveInfo> {
  const response = await apiClient.post<SystemArchiveInfo>('/console/system/diagnostics')
  return response.data
}

export async function downloadDiagnosticBundle(bundle: SystemArchiveInfo): Promise<void> {
  await downloadArchive(`/console/system/diagnostics/${encodeURIComponent(bundle.id)}/download`, bundle.path)
}

async function downloadArchive(path: string, filename: string): Promise<void> {
  const response = await apiClient.get<Blob>(path, { responseType: 'blob' })
  const blob = new Blob([response.data], { type: 'application/gzip' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}
