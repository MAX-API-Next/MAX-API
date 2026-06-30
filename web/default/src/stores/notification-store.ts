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
import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface NotificationUserState {
  lastReadNotice: string
  readAnnouncementKeys: string[]
  closedUntilDate: string | null
}

interface NotificationState {
  byUser: Record<string, NotificationUserState>
  // Auto-opened content signatures for the current page session.
  autoOpenedSignatures: string[]

  // Actions
  markNoticeRead: (userScope: string, noticeContent: string) => void
  markAnnouncementsRead: (userScope: string, keys: string[]) => void
  rememberAutoOpenedSignature: (contentSignature: string) => boolean
  setClosedUntilDate: (userScope: string, date: string | null) => void
  getUserState: (userScope: string) => NotificationUserState
  isAnnouncementRead: (userScope: string, key: string) => boolean
  isNoticeClosed: (userScope: string) => boolean
}

const defaultNotificationUserState: NotificationUserState = {
  lastReadNotice: '',
  readAnnouncementKeys: [],
  closedUntilDate: null,
}

function getStoredUserState(
  byUser: Record<string, NotificationUserState>,
  userScope: string
): NotificationUserState {
  return byUser[userScope] ?? defaultNotificationUserState
}

function setUserState(
  state: NotificationState,
  userScope: string,
  patch: Partial<NotificationUserState>
): Pick<NotificationState, 'byUser'> {
  const current = getStoredUserState(state.byUser, userScope)

  return {
    byUser: {
      ...state.byUser,
      [userScope]: {
        ...current,
        ...patch,
      },
    },
  }
}

function migratePersistedNotificationState(
  persisted: unknown,
  version: number
): Partial<NotificationState> {
  if (
    version >= 1 ||
    !persisted ||
    typeof persisted !== 'object' ||
    'byUser' in persisted
  ) {
    return persisted as Partial<NotificationState>
  }

  const legacy = persisted as Partial<NotificationUserState>

  return {
    byUser: {
      anonymous: {
        lastReadNotice: legacy.lastReadNotice ?? '',
        readAnnouncementKeys: legacy.readAnnouncementKeys ?? [],
        closedUntilDate: legacy.closedUntilDate ?? null,
      },
    },
  }
}

/**
 * Notification store for tracking read status of Notice and Announcements
 * Persists to localStorage to maintain state across sessions
 */
export const useNotificationStore = create<NotificationState>()(
  persist(
    (set, get) => ({
      byUser: {},
      autoOpenedSignatures: [],

      markNoticeRead: (userScope: string, noticeContent: string) => {
        // Persist the full trimmed content so edits beyond 100 chars register
        const normalizedContent = noticeContent.trim()
        set((state) =>
          setUserState(state, userScope, {
            lastReadNotice: normalizedContent,
          })
        )
      },

      markAnnouncementsRead: (userScope: string, keys: string[]) => {
        set((state) => {
          const current = getStoredUserState(state.byUser, userScope)

          return setUserState(state, userScope, {
            readAnnouncementKeys: [
              ...new Set([...current.readAnnouncementKeys, ...keys]),
            ],
          })
        })
      },

      rememberAutoOpenedSignature: (contentSignature: string) => {
        if (!contentSignature) {
          return false
        }

        if (get().autoOpenedSignatures.includes(contentSignature)) {
          return false
        }

        set((state) => ({
          autoOpenedSignatures: [
            ...state.autoOpenedSignatures,
            contentSignature,
          ],
        }))
        return true
      },

      setClosedUntilDate: (userScope: string, date: string | null) => {
        set((state) => setUserState(state, userScope, { closedUntilDate: date }))
      },

      getUserState: (userScope: string) => {
        return getStoredUserState(get().byUser, userScope)
      },

      isAnnouncementRead: (userScope: string, key: string) => {
        return get()
          .getUserState(userScope)
          .readAnnouncementKeys.includes(key)
      },

      isNoticeClosed: (userScope: string) => {
        const { closedUntilDate } = get().getUserState(userScope)
        if (!closedUntilDate) return false

        const today = new Date().toDateString()
        return closedUntilDate === today
      },
    }),
    {
      name: 'notification-storage',
      version: 1,
      migrate: migratePersistedNotificationState,
      partialize: (state) => ({
        byUser: state.byUser,
      }),
    }
  )
)
