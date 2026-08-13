const navigationCancellationErrors = new Set([
  'net::ERR_ABORTED',
  'NS_BINDING_ABORTED',
  'cancelled'
])

export function isNavigationCancellationError(errorText: string | null | undefined): boolean {
  return typeof errorText === 'string' && navigationCancellationErrors.has(errorText)
}
