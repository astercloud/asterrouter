import { apiClient } from './client'
import type { Plugin, PluginCatalog } from '@/types'

export async function getPluginCatalog(): Promise<PluginCatalog> {
  const response = await apiClient.get<PluginCatalog>('/admin/plugins')
  return response.data
}

export async function enablePlugin(id: string): Promise<Plugin> {
  const response = await apiClient.post<Plugin>(`/admin/plugins/${encodeURIComponent(id)}/enable`)
  return response.data
}

export async function disablePlugin(id: string): Promise<Plugin> {
  const response = await apiClient.post<Plugin>(`/admin/plugins/${encodeURIComponent(id)}/disable`)
  return response.data
}
