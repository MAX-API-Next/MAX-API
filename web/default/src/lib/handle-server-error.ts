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
import i18next from 'i18next'
import { toast } from 'sonner'

interface SafeErrorDebugInfo {
  name: string
  message: string
  code?: string
  status?: number
}

interface HandleServerErrorOptions {
  fallback?: string
}

export function getSafeErrorDebugInfo(error: unknown): SafeErrorDebugInfo {
  if (error instanceof AxiosError) {
    return {
      name: error.name || 'AxiosError',
      message: error.message,
      code: error.code,
      status: error.response?.status,
    }
  }
  if (error instanceof Error) {
    return { name: error.name, message: error.message }
  }
  return { name: 'UnknownError', message: 'Unknown error' }
}

export function handleServerError(
  error: unknown,
  options: HandleServerErrorOptions = {}
) {
  if (import.meta.env.DEV) {
    // eslint-disable-next-line no-console
    console.error('[handleServerError]', getSafeErrorDebugInfo(error))
  }

  let errMsg = options.fallback || i18next.t('Something went wrong!')

  if (
    error &&
    typeof error === 'object' &&
    'status' in error &&
    Number(error.status) === 204
  ) {
    errMsg = i18next.t('Content not found.')
  }

  if (error instanceof AxiosError) {
    const responseTitle = (
      error.response?.data as { title?: unknown } | undefined
    )?.title
    if (typeof responseTitle === 'string' && responseTitle) {
      errMsg = responseTitle
    }
  }

  toast.error(errMsg)
}
