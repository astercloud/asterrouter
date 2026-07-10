import { apiClient } from './client'
import type { AuthUser, LoginResult } from '@/types'

export async function login(username: string, password: string): Promise<LoginResult> {
  const response = await apiClient.post<LoginResult>('/auth/login', { username, password })
  return response.data
}

export async function getCurrentUser(): Promise<AuthUser> {
  const response = await apiClient.get<AuthUser>('/auth/me')
  return response.data
}
