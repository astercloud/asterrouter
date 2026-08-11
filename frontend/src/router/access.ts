import type { AuthUser } from '@/types'

export type ProductEntry = 'console' | 'portal'

export function entryForUser(user: AuthUser | null | undefined): string {
  return user?.role === 'developer' ? '/portal/overview' : '/console/workbench'
}

export function productEntryForPath(path: string): ProductEntry | null {
  if (path.startsWith('/console')) return 'console'
  if (path.startsWith('/portal')) return 'portal'
  return null
}

export function canAccessEntry(user: AuthUser | null | undefined, entry: ProductEntry): boolean {
  if (!user) return false
  if (entry === 'portal') return true
  return user.role !== 'developer'
}
