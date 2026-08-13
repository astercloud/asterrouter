import { describe, expect, it } from 'vitest'
import { isNavigationCancellationError } from './browser-errors'

describe('isNavigationCancellationError', () => {
  it.each([
    'net::ERR_ABORTED',
    'NS_BINDING_ABORTED',
    'cancelled'
  ])('recognizes browser navigation cancellation error %s', (errorText) => {
    expect(isNavigationCancellationError(errorText)).toBe(true)
  })

  it.each([
    undefined,
    null,
    '',
    'net::ERR_FAILED',
    'NS_ERROR_NET_RESET',
    'cancelled by server'
  ])('keeps non-navigation failure %s observable', (errorText) => {
    expect(isNavigationCancellationError(errorText)).toBe(false)
  })
})
