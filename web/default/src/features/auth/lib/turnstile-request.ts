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

export const TURNSTILE_TOKEN_HEADER = 'X-Turnstile-Token'

export function getTurnstileHeaders(token?: string): Record<string, string> {
  const value = token?.trim()
  return value ? { [TURNSTILE_TOKEN_HEADER]: value } : {}
}

export type ConsumeTurnstileTokenResult = {
  submittedToken: string
  nextToken: string
  shouldRefreshWidget: boolean
}

export function consumeTurnstileToken(
  token: string,
  enabled: boolean
): ConsumeTurnstileTokenResult {
  return {
    submittedToken: token,
    nextToken: enabled ? '' : token,
    shouldRefreshWidget: enabled,
  }
}
