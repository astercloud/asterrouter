import { apiClient } from './client'
import type { AdminSettings, LocaleInfo, PublicSettings } from '@/types'

export async function getPublicSettings(): Promise<PublicSettings> {
  const response = await apiClient.get<PublicSettings>('/settings/public')
  return response.data
}

export async function getAdminSettings(): Promise<AdminSettings> {
  const response = await apiClient.get<AdminSettings>('/admin/settings')
  return response.data
}

export async function updateAdminSettings(payload: AdminSettings): Promise<AdminSettings> {
  const response = await apiClient.put<AdminSettings>('/admin/settings', payload)
  return response.data
}

export async function applySetupProfile(profile: string): Promise<AdminSettings> {
  const response = await apiClient.post<AdminSettings>('/setup/profile', { profile })
  return response.data
}

export async function getLocales(): Promise<LocaleInfo[]> {
  const response = await apiClient.get<LocaleInfo[]>('/i18n/locales')
  return response.data
}
