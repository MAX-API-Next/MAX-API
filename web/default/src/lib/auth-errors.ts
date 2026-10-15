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
import { AxiosError } from 'axios'

export function isStaleAuthResponse(error: unknown): boolean {
  return error instanceof AxiosError && error.config?.staleAuthResponse === true
}

export function handleQueryAuthenticationError(
  error: unknown,
  onExpired: () => void
): void {
  if (
    error instanceof AxiosError &&
    error.response?.status === 401 &&
    !isStaleAuthResponse(error)
  )
    onExpired()
}
