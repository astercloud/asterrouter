import { apiClient } from './client'
import type {
  APIKeyClientConfig,
  APIKeyRecord,
  ClientVerificationRequest,
  ClientVerificationResult,
  CompatibilityManifest,
  OnboardingAPIKeyRequest,
  OnboardingAPIKeyResult,
  OnboardingClient,
  OnboardingModelSourceRequest,
  OnboardingModelSourceResult,
  OnboardingPublishedModelRequest,
  OnboardingPublishedModelResult,
  OnboardingSession,
  OnboardingVerificationResult
} from '@/types'

function idempotencyHeaders(idempotencyKey: string) {
  return { headers: { 'Idempotency-Key': idempotencyKey } }
}

export async function startOnboardingSession(idempotencyKey: string): Promise<OnboardingSession> {
  return (await apiClient.post<OnboardingSession>('/onboarding/sessions', null, idempotencyHeaders(idempotencyKey))).data
}

export async function getOnboardingSession(id: string): Promise<OnboardingSession> {
  return (await apiClient.get<OnboardingSession>(`/onboarding/sessions/${encodeURIComponent(id)}`)).data
}

export async function getCompatibilityManifest(): Promise<CompatibilityManifest> {
  return (await apiClient.get<CompatibilityManifest>('/onboarding/compatibility-records')).data
}

export async function connectOnboardingModelSource(id: string, request: OnboardingModelSourceRequest): Promise<OnboardingModelSourceResult> {
  return (await apiClient.post<OnboardingModelSourceResult>(`/onboarding/sessions/${encodeURIComponent(id)}/model-source`, request)).data
}

export async function publishOnboardingModel(id: string, request: OnboardingPublishedModelRequest): Promise<OnboardingPublishedModelResult> {
  return (await apiClient.post<OnboardingPublishedModelResult>(`/onboarding/sessions/${encodeURIComponent(id)}/published-model`, request)).data
}

export async function createOnboardingAPIKey(id: string, request: OnboardingAPIKeyRequest): Promise<OnboardingAPIKeyResult> {
  return (await apiClient.post<OnboardingAPIKeyResult>(`/onboarding/sessions/${encodeURIComponent(id)}/api-key`, request)).data
}

export async function verifyOnboardingClient(id: string, request: ClientVerificationRequest, idempotencyKey: string): Promise<OnboardingVerificationResult> {
  return (await apiClient.post<OnboardingVerificationResult>(`/onboarding/sessions/${encodeURIComponent(id)}/verification`, request, idempotencyHeaders(idempotencyKey))).data
}

export async function getOnboardingAPIKey(id: string): Promise<APIKeyRecord> {
  return (await apiClient.get<APIKeyRecord>(`/console/api-keys/${encodeURIComponent(id)}`)).data
}

export async function getAPIKeyClientConfig(id: string, client: OnboardingClient, model: string): Promise<APIKeyClientConfig> {
  return (await apiClient.get<APIKeyClientConfig>(`/console/api-keys/${encodeURIComponent(id)}/client-config`, { params: { client, model } })).data
}

export async function verifyAPIKeyClient(id: string, request: ClientVerificationRequest, idempotencyKey: string): Promise<ClientVerificationResult> {
  return (await apiClient.post<ClientVerificationResult>(`/console/api-keys/${encodeURIComponent(id)}/client-verifications`, request, idempotencyHeaders(idempotencyKey))).data
}
