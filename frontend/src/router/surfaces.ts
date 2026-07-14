import type { AuthUser } from '@/types'

export type Surface = 'personal' | 'relay_operator' | 'enterprise' | 'platform' | 'portal' | 'customer'

const pathSurface: Array<[string, Surface]> = [
  ['/console', 'personal'],
  ['/operator', 'relay_operator'],
  ['/admin', 'enterprise'],
  ['/platform', 'platform'],
  ['/portal', 'portal'],
  ['/customer', 'customer']
]

export function surfaceForPath(path: string): Surface | null {
  for (const [prefix, surface] of pathSurface) {
    if (path.startsWith(prefix)) return surface
  }
  return null
}

export function canAccessSurface(user: AuthUser | null | undefined, surface: Surface): boolean {
	if (!user) return false
	if (user.allowed_surfaces !== undefined) return user.allowed_surfaces.includes(surface)
	// Older persisted sessions predate allowed_surfaces. Use a conservative
	// approximation until /auth/me refreshes the server-derived summary.
	if (surface === 'portal' || surface === 'customer') return true
	if (surface === 'enterprise') return user.role !== 'developer'
	if (surface === 'platform') return false
	return user.role === 'super_admin' || user.role === 'demo_admin'
}

export function defaultSurfaceRoute(enabledProfiles: string[], defaultProfile: string, user: AuthUser | null | undefined): string {
	const profile = enabledProfiles.includes(defaultProfile) ? defaultProfile : enabledProfiles[0]
	if (!user) {
		if (profile === 'personal') return '/console/overview'
		if (profile === 'relay_operator') return '/customer/overview'
		if (profile === 'platform') return '/platform/overview'
		return '/admin/dashboard'
	}
	if (profile === 'personal' && canAccessSurface(user, 'personal')) return '/console/overview'
  if (profile === 'relay_operator') {
    if (canAccessSurface(user, 'relay_operator')) return '/operator/overview'
    if (canAccessSurface(user, 'customer')) return '/customer/overview'
  }
  if (profile === 'enterprise') {
    if (canAccessSurface(user, 'enterprise')) return '/admin/dashboard'
    if (canAccessSurface(user, 'portal')) return '/portal/overview'
  }
  if (profile === 'platform' && canAccessSurface(user, 'platform')) return '/platform/overview'
  const fallback: Array<[string, Surface, string]> = [
    ['personal', 'personal', '/console/overview'],
    ['relay_operator', 'customer', '/customer/overview'],
    ['enterprise', 'portal', '/portal/overview'],
    ['platform', 'platform', '/platform/overview']
  ]
  for (const [profileName, surface, route] of fallback) {
    if (enabledProfiles.includes(profileName) && canAccessSurface(user, surface)) return route
  }
  return '/login'
}
