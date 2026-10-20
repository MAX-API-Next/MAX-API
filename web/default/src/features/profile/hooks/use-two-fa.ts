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
import { useEffect, useCallback } from 'react'
import { useAuthStore } from '@/stores/auth-store'
import { get2FAStatus } from '@/lib/api'
import { getSafeErrorDebugInfo } from '@/lib/handle-server-error'
import { useReadScope } from '@/hooks/use-read-scope'
import { useResettableState } from '@/hooks/use-resettable-state'
import type { TwoFAStatus } from '../types'

// ============================================================================
// Two-FA Hook
// ============================================================================

const DEFAULT_STATUS: TwoFAStatus = {
  enabled: false,
  locked: false,
  backup_codes_remaining: 0,
}

export function useTwoFA(enabled = true) {
  const sessionRevision = useAuthStore((state) => state.sessionRevision)
  const scopeKey = JSON.stringify([sessionRevision, enabled])
  const beginRead = useReadScope(scopeKey)
  const [loading, setLoading] = useResettableState(enabled, scopeKey)
  const [status, setStatus] = useResettableState<TwoFAStatus>(
    DEFAULT_STATUS,
    scopeKey
  )

  const loadStatus = useCallback(() => {
    if (!enabled) return Promise.resolve()
    const current = beginRead()
    if (!current()) return Promise.resolve()
    return get2FAStatus()
      .then((response) => {
        if (current() && response.success && response.data)
          setStatus(response.data)
      })
      .catch((error) => {
        if (!current()) return
        // eslint-disable-next-line no-console
        console.error(
          'Failed to fetch 2FA status:',
          getSafeErrorDebugInfo(error)
        )
      })
      .finally(() => {
        if (current()) setLoading(false)
      })
  }, [enabled, beginRead, setStatus, setLoading])
  const fetchStatus = useCallback(() => {
    if (!enabled) return Promise.resolve()
    setLoading(true)
    return loadStatus()
  }, [enabled, loadStatus, setLoading])
  useEffect(() => {
    void loadStatus()
  }, [loadStatus])

  return {
    status,
    loading,
    refetch: fetchStatus,
  }
}
