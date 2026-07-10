export function datetimeLocalToISOString(value: string): string | undefined {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const date = new Date(trimmed)
  if (Number.isNaN(date.getTime())) return undefined
  return date.toISOString()
}
