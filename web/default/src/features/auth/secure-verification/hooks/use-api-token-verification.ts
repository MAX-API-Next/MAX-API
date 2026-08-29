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
import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useSecureVerificationGate } from '../components/secure-verification-provider'

const defaultApiTokenVerificationDescription =
  'Confirm your identity before sending an API key to a chat client.'

export function useApiTokenVerification(
  description?: string
): <T>(apiCall: () => Promise<T>) => Promise<T | null> {
  const { t } = useTranslation()
  const { withVerification } = useSecureVerificationGate()

  return useCallback(
    <T>(apiCall: () => Promise<T>): Promise<T | null> =>
      withVerification(apiCall, {
        scope: 'api_token',
        title: t('Security verification'),
        description: description ?? t(defaultApiTokenVerificationDescription),
      }),
    [description, t, withVerification]
  )
}
