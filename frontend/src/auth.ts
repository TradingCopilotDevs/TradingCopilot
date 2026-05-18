const TOKEN_STORAGE_KEY = 'tradingcopilot_token'

export function getAuthToken(): string | null {
  return localStorage.getItem(TOKEN_STORAGE_KEY)
}

export function setAuthToken(token: string) {
  localStorage.setItem(TOKEN_STORAGE_KEY, token)
}

export function clearAuthToken() {
  localStorage.removeItem(TOKEN_STORAGE_KEY)
}

export function getAuthTokenExpiresAt(token: string | null): number | null {
  if (!token) return null
  const parts = token.split('.')
  if (parts.length < 2) return null
  try {
    const payload = JSON.parse(decodeBase64Url(parts[1])) as { exp?: unknown }
    return typeof payload.exp === 'number' ? payload.exp * 1000 : null
  } catch {
    return null
  }
}

export function isAuthTokenUsable(token: string | null) {
  const expiresAt = getAuthTokenExpiresAt(token)
  return expiresAt !== null && expiresAt > Date.now()
}

function decodeBase64Url(value: string) {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = normalized.padEnd(normalized.length + ((4 - (normalized.length % 4)) % 4), '=')
  return decodeURIComponent(
    atob(padded)
      .split('')
      .map((char) => `%${char.charCodeAt(0).toString(16).padStart(2, '0')}`)
      .join('')
  )
}
