import axios, { AxiosError } from 'axios'
import type { ApiResponse } from '@/types'
import { getLocale } from '@/i18n'

export const apiClient = axios.create({
  baseURL: '/api/v1',
  timeout: 20000,
  headers: {
    'Content-Type': 'application/json'
  }
})

apiClient.interceptors.request.use((config) => {
  config.headers['Accept-Language'] = getLocale()
  const token = localStorage.getItem('asterrouter_admin_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

apiClient.interceptors.response.use(
  (response) => {
    const payload = response.data as ApiResponse<unknown>
    if (payload && typeof payload === 'object' && 'code' in payload) {
      if (payload.code === 0) {
        response.data = payload.data
        return response
      }
      return Promise.reject(new Error(payload.message || 'Request failed'))
    }
    return response
  },
  (error: AxiosError<ApiResponse<unknown>>) => {
    const message = error.response?.data?.message || error.message || 'Network error'
    return Promise.reject(new Error(message))
  }
)
