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
import { useState, useEffect, useCallback } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { getSafeErrorDebugInfo } from '@/lib/handle-server-error'
import { useReadScope } from '@/hooks/use-read-scope'
import { useResettableState } from '@/hooks/use-resettable-state'
import { getUserProfile, updateUserProfile, updateUserSettings } from '../api'
import type {
  UserProfile,
  UpdateUserRequest,
  UpdateUserSettingsRequest,
} from '../types'

// ============================================================================
// Profile Hook
// ============================================================================

export function useProfile() {
  const sessionRevision = useAuthStore((state) => state.sessionRevision)
  const beginRead = useReadScope(sessionRevision)
  const [profile, setProfile] = useResettableState<UserProfile | null>(
    null,
    sessionRevision
  )
  const [loading, setLoading] = useResettableState(true, sessionRevision)
  const [updating, setUpdating] = useState(false)

  const loadProfile = useCallback(
    (silent = false) => {
      const current = beginRead()
      if (!current()) return Promise.resolve()
      return getUserProfile()
        .then((response) => {
          if (current() && response.success && response.data)
            setProfile(response.data)
        })
        .catch((error) => {
          if (!current()) return
          // eslint-disable-next-line no-console
          console.error(
            'Failed to fetch profile:',
            getSafeErrorDebugInfo(error)
          )
          if (!silent) toast.error(i18next.t('Failed to load profile'))
        })
        .finally(() => {
          if (current()) setLoading(false)
        })
    },
    [beginRead, setProfile, setLoading]
  )
  const fetchProfile = useCallback(
    (silent = false) => {
      if (!silent) setLoading(true)
      return loadProfile(silent)
    },
    [loadProfile, setLoading]
  )

  // Refresh profile silently (without loading state)
  const refreshProfile = useCallback(async () => {
    await fetchProfile(true)
  }, [fetchProfile])

  // Update user profile
  const updateProfile = useCallback(
    async (data: UpdateUserRequest): Promise<boolean> => {
      try {
        setUpdating(true)
        const response = await updateUserProfile(data)

        if (response.success) {
          toast.success(i18next.t('Profile updated successfully'))
          await refreshProfile() // Refresh profile silently
          return true
        }

        toast.error(response.message || i18next.t('Failed to update profile'))
        return false
      } catch (error) {
        // eslint-disable-next-line no-console
        console.error('Failed to update profile:', getSafeErrorDebugInfo(error))
        toast.error(i18next.t('Failed to update profile'))
        return false
      } finally {
        setUpdating(false)
      }
    },
    [refreshProfile]
  )

  // Update user settings
  const updateSettings = useCallback(
    async (data: UpdateUserSettingsRequest): Promise<boolean> => {
      try {
        setUpdating(true)
        const response = await updateUserSettings(data)

        if (response.success) {
          toast.success(i18next.t('Settings updated successfully'))
          await refreshProfile() // Refresh profile silently
          return true
        }

        toast.error(response.message || i18next.t('Failed to update settings'))
        return false
      } catch (error) {
        // eslint-disable-next-line no-console
        console.error(
          'Failed to update settings:',
          getSafeErrorDebugInfo(error)
        )
        toast.error(i18next.t('Failed to update settings'))
        return false
      } finally {
        setUpdating(false)
      }
    },
    [refreshProfile]
  )

  // Initial fetch
  useEffect(() => {
    void loadProfile()
  }, [loadProfile])

  return {
    profile,
    loading,
    updating,
    fetchProfile,
    refreshProfile,
    updateProfile,
    updateSettings,
  }
}
