import { apiClient } from './client'
import type { AccountProfile, AccountSecurityUpdate, TOTPSetup } from '@/types'

export async function getAccountProfile(): Promise<AccountProfile> {
	return (await apiClient.get<AccountProfile>('/account/profile')).data
}

export async function updateAccountProfile(displayName: string, avatarDataURL: string): Promise<AccountProfile> {
	return (await apiClient.put<AccountProfile>('/account/profile', { display_name: displayName, avatar_data_url: avatarDataURL })).data
}

export async function changeAccountPassword(currentPassword: string, newPassword: string): Promise<AccountSecurityUpdate> {
	return (await apiClient.put<AccountSecurityUpdate>('/account/password', { current_password: currentPassword, new_password: newPassword })).data
}

export async function beginTOTPSetup(): Promise<TOTPSetup> {
	return (await apiClient.post<TOTPSetup>('/account/totp/setup')).data
}

export async function confirmTOTP(code: string): Promise<AccountSecurityUpdate> {
	return (await apiClient.post<AccountSecurityUpdate>('/account/totp/confirm', { code })).data
}

export async function generateTOTPRecoveryCodes(): Promise<AccountSecurityUpdate> {
	return (await apiClient.post<AccountSecurityUpdate>('/account/totp/recovery-codes')).data
}

export async function disableTOTP(code: string): Promise<AccountSecurityUpdate> {
	return (await apiClient.delete<AccountSecurityUpdate>('/account/totp', { data: { code } })).data
}

export async function revokeOtherAccountSessions(): Promise<AccountSecurityUpdate> {
	return (await apiClient.post<AccountSecurityUpdate>('/account/sessions/revoke-others')).data
}

export async function unbindAccountIdentity(provider: string): Promise<AccountProfile> {
	return (await apiClient.delete<AccountProfile>(`/account/identities/${encodeURIComponent(provider)}`)).data
}

export async function beginAccountIdentityBinding(provider: string, returnPath: string): Promise<string> {
	return (await apiClient.post<{ authorization_url: string }>(`/account/identities/${encodeURIComponent(provider)}/bind`, { return_path: returnPath })).data.authorization_url
}
