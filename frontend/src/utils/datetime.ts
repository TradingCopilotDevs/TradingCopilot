const APP_TIME_ZONE = 'Asia/Shanghai'

export interface Utc8DateParts {
  year: string
  month: string
  day: string
  hour: string
  minute: string
  second: string
}

function normalizeIsoLike(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return trimmed

  if (/^\d{4}-\d{2}-\d{2}$/.test(trimmed)) {
    return `${trimmed}T00:00:00+08:00`
  }
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(trimmed)) {
    return `${trimmed.replace(' ', 'T')}+08:00`
  }
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?$/.test(trimmed)) {
    return `${trimmed}+08:00`
  }
  return trimmed
}

export function toDateUtc8(value: string | number | Date | null | undefined): Date | null {
  if (value === null || value === undefined || value === '') return null
  if (value instanceof Date) return Number.isNaN(value.getTime()) ? null : value

  const normalized = typeof value === 'string' ? normalizeIsoLike(value) : value
  const date = new Date(normalized)
  return Number.isNaN(date.getTime()) ? null : date
}

export function formatDateTimeUtc8(value: string | number | Date | null | undefined): string {
  const map = getUtc8DateParts(value)
  if (!map) return '-'
  return `${map.year}-${map.month}-${map.day} ${map.hour}:${map.minute}:${map.second} UTC+8`
}

export function getUtc8DateParts(value: string | number | Date | null | undefined): Utc8DateParts | null {
  const date = toDateUtc8(value)
  if (!date) return null

  const parts = new Intl.DateTimeFormat('zh-CN', {
    timeZone: APP_TIME_ZONE,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).formatToParts(date)

  const map = Object.fromEntries(parts.map((part) => [part.type, part.value])) as Record<string, string>
  return {
    year: map.year || '0000',
    month: map.month || '00',
    day: map.day || '00',
    hour: map.hour || '00',
    minute: map.minute || '00',
    second: map.second || '00',
  }
}

export function toUnixSecondsUtc8(value: string | number | Date | null | undefined): number | null {
  const date = toDateUtc8(value)
  if (!date) return null
  return Math.floor(date.getTime() / 1000)
}

export function parseBusinessDate(value: string | null | undefined): { year: number; month: number; day: number } | null {
  const match = String(value || '')
    .trim()
    .match(/^(\d{4})-(\d{2})-(\d{2})/)
  if (!match) return null
  return {
    year: Number(match[1]),
    month: Number(match[2]),
    day: Number(match[3]),
  }
}
