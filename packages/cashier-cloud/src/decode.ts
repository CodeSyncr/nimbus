/**
 * Wire decoders.
 *
 * Cloud's API is snake_case on the wire and the SDK surface is camelCase, so
 * every field is read through `pick`, which accepts either. It also accepts Go
 * PascalCase, which costs nothing and means a Nimbus app proxying Cloud's JSON
 * through its own types does not have to normalise first.
 */

/** Reads the first present spelling of a field: snake_case, camelCase or Go's PascalCase. */
export function pick<T = unknown>(obj: any, ...names: string[]): T | undefined {
  if (!obj || typeof obj !== 'object') return undefined
  for (const name of names) {
    for (const variant of variants(name)) {
      if (obj[variant] !== undefined && obj[variant] !== null) return obj[variant] as T
    }
  }
  return undefined
}

function variants(name: string): string[] {
  const camel = name.replace(/_([a-z0-9])/g, (_, c: string) => c.toUpperCase())
  const snake = name.replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase()
  const pascal = camel.charAt(0).toUpperCase() + camel.slice(1)
  // Go exports acronyms whole — ID, URL, IDs — so offer those too.
  const goStyle = pascal
    .replace(/Id(s?)\b/g, 'ID$1')
    .replace(/Url\b/g, 'URL')
  return [...new Set([name, snake, camel, pascal, goStyle])]
}

/** Parses an RFC3339 string, a millisecond epoch, or a Date. Go's zero time → null. */
export function asDate(value: unknown): Date | null {
  if (value === undefined || value === null || value === '') return null
  if (value instanceof Date) return Number.isNaN(value.getTime()) ? null : value
  if (typeof value === 'number') return new Date(value)
  if (typeof value !== 'string') return null
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return null
  // Go marshals a zero time.Time as year 1 — that is "never", not a date.
  return d.getUTCFullYear() <= 1 ? null : d
}

export function asString(value: unknown, fallback = ''): string {
  if (value === undefined || value === null) return fallback
  return typeof value === 'string' ? value : String(value)
}

export function asNumber(value: unknown, fallback = 0): number {
  if (typeof value === 'number') return value
  if (typeof value === 'string' && value.trim() !== '') {
    const n = Number(value)
    if (!Number.isNaN(n)) return n
  }
  return fallback
}

export function asBool(value: unknown, fallback = false): boolean {
  if (typeof value === 'boolean') return value
  if (typeof value === 'string') return value === 'true' || value === '1'
  if (typeof value === 'number') return value !== 0
  return fallback
}
