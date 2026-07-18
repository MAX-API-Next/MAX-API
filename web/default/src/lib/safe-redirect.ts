/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

const redirectBase = 'https://max-api.invalid'

export function normalizeInternalRedirect(
  target: string | null | undefined,
  fallback = '/dashboard'
): string {
  const candidate = target?.trim()
  if (!candidate || !candidate.startsWith('/') || candidate.startsWith('//')) {
    return fallback
  }

  try {
    const parsed = new URL(candidate, redirectBase)
    if (parsed.origin !== redirectBase) return fallback
    return `${parsed.pathname}${parsed.search}${parsed.hash}`
  } catch {
    return fallback
  }
}
