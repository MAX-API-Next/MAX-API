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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useNotificationStore } from '@/stores/notification-store'
import { useAuthStore } from '@/stores/auth-store'
import { getNotice } from '@/lib/api'
import { useStatus } from '@/hooks/use-status'
import {
  getAnnouncementKey,
  getAutoNotificationTab,
  getNotificationContentSignature,
  getNotificationUserScope,
  getScopedNotificationAutoOpenSignature,
  shouldAutoOpenNotifications,
  shouldRememberOpenNotificationSignature,
  type NotificationTab,
} from './notification-utils'

type RefetchNotice = UseQueryResult<
  Awaited<ReturnType<typeof getNotice>>
>['refetch']

export type UseNotificationsResult = {
  activeTab: NotificationTab
  announcements: Record<string, unknown>[]
  closeForToday: () => void
  closePopover: () => void
  loading: boolean
  notice: string
  openPopover: (tab?: NotificationTab) => void
  popoverOpen: boolean
  refetchNotice: RefetchNotice
  setActiveTab: (tab: NotificationTab) => void
  setPopoverOpen: (open: boolean) => void
  unreadAnnouncementsCount: number
  unreadCount: number
  unreadNoticeCount: number
}

/**
 * Hook to manage notifications (Notice + Announcements)
 * Provides unread counts and read status management
 */
export function useNotifications(): UseNotificationsResult {
  const [popoverOpen, setPopoverOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<NotificationTab>('notice')
  const openedUserScopeRef = useRef<string | null>(null)
  const userId = useAuthStore((state) => state.auth.user?.id ?? null)
  const userScope = getNotificationUserScope(userId)

  // Fetch Notice from API
  const {
    data: noticeResponse,
    isLoading: noticeLoading,
    refetch: refetchNotice,
  } = useQuery({
    queryKey: ['notice'],
    queryFn: getNotice,
    staleTime: 1000 * 60 * 5, // 5 minutes
  })

  // Fetch Announcements from status
  const { status, loading: statusLoading } = useStatus()
  const announcementsEnabled = status?.announcements_enabled ?? false
  const statusAnnouncements = status?.announcements
  const announcements: Record<string, unknown>[] = useMemo(() => {
    return announcementsEnabled
      ? ((statusAnnouncements || []) as Record<string, unknown>[]).slice(0, 20)
      : []
  }, [announcementsEnabled, statusAnnouncements])

  // Notification store
  const {
    markNoticeRead,
    markAnnouncementsRead,
    autoOpenedSignatures,
    rememberAutoOpenedSignature,
    setClosedUntilDate,
    isAnnouncementRead,
    isNoticeClosed,
  } = useNotificationStore()
  const { lastReadNotice } = useNotificationStore((state) =>
    state.getUserState(userScope)
  )

  // Extract notice content
  const noticeContent = noticeResponse?.success
    ? (noticeResponse.data || '').trim()
    : ''

  // Calculate unread counts
  const unreadCounts = useMemo(() => {
    const noticeUnread =
      noticeContent && noticeContent !== lastReadNotice ? 1 : 0

    const announcementsUnread = announcements.filter(
      (item: Record<string, unknown>) => {
        const key = getAnnouncementKey(item)
        return !isAnnouncementRead(userScope, key)
      }
    ).length

    return {
      notice: noticeUnread,
      announcements: announcementsUnread,
      total: noticeUnread + announcementsUnread,
    }
  }, [
    noticeContent,
    lastReadNotice,
    announcements,
    isAnnouncementRead,
    userScope,
  ])

  const loading = noticeLoading || statusLoading
  const contentSignature = useMemo(
    () => getNotificationContentSignature(noticeContent, announcements),
    [noticeContent, announcements]
  )
  const autoOpenSignature = useMemo(
    () => getScopedNotificationAutoOpenSignature(userId, contentSignature),
    [contentSignature, userId]
  )

  const markAnnouncementsAsRead = useCallback(() => {
    if (announcements.length > 0) {
      const allKeys = announcements.map((item: Record<string, unknown>) =>
        getAnnouncementKey(item)
      )
      markAnnouncementsRead(userScope, allKeys)
    }
  }, [announcements, markAnnouncementsRead, userScope])

  const markTabAsRead = useCallback(
    (tab: NotificationTab) => {
      if (tab === 'notice' && noticeContent) {
        markNoticeRead(userScope, noticeContent)
      }

      if (tab === 'announcements') {
        markAnnouncementsAsRead()
      }
    },
    [markAnnouncementsAsRead, markNoticeRead, noticeContent, userScope]
  )

  // Handle popover open
  const handleOpenPopover = useCallback(
    (tab?: NotificationTab) => {
      const nextTab = tab || activeTab

      if (autoOpenSignature !== '') {
        rememberAutoOpenedSignature(autoOpenSignature)
      }
      markTabAsRead(nextTab)
      setActiveTab(nextTab)
      openedUserScopeRef.current = userScope
      setPopoverOpen(true)
    },
    [
      activeTab,
      autoOpenSignature,
      markTabAsRead,
      rememberAutoOpenedSignature,
      userScope,
    ]
  )

  const handlePopoverOpenChange = useCallback(
    (open: boolean) => {
      if (open) {
        handleOpenPopover(activeTab)
        return
      }

      setPopoverOpen(false)
      openedUserScopeRef.current = null
    },
    [activeTab, handleOpenPopover]
  )

  const closeForToday = useCallback(() => {
    setClosedUntilDate(userScope, new Date().toDateString())
    openedUserScopeRef.current = null
    setPopoverOpen(false)
  }, [setClosedUntilDate, userScope])

  // Handle tab change - mark announcements as read when switching to that tab
  const handleTabChange = useCallback(
    (tab: NotificationTab) => {
      setActiveTab(tab)
      markTabAsRead(tab)
    },
    [markTabAsRead]
  )

  useEffect(() => {
    if (
      popoverOpen &&
      shouldRememberOpenNotificationSignature({
        contentSignature: autoOpenSignature,
        openedUserScope: openedUserScopeRef.current,
        userScope,
      })
    ) {
      rememberAutoOpenedSignature(autoOpenSignature)
    }
  }, [autoOpenSignature, popoverOpen, rememberAutoOpenedSignature, userScope])

  useEffect(() => {
    if (
      !shouldAutoOpenNotifications({
        autoOpenedSignatures,
        contentSignature: autoOpenSignature,
        isClosedToday: isNoticeClosed(userScope),
        loading,
        popoverOpen,
      })
    ) {
      return
    }

    const autoTab = getAutoNotificationTab({
      hasAnnouncements: announcements.length > 0,
      hasNotice: noticeContent !== '',
      unreadCounts,
    })

    if (!autoTab) {
      return
    }

    const timeoutId = window.setTimeout(() => {
      if (!rememberAutoOpenedSignature(autoOpenSignature)) {
        return
      }

      handleOpenPopover(autoTab)
    }, 0)

    return () => {
      window.clearTimeout(timeoutId)
    }
  }, [
    autoOpenSignature,
    autoOpenedSignatures,
    announcements.length,
    handleOpenPopover,
    isNoticeClosed,
    loading,
    noticeContent,
    popoverOpen,
    rememberAutoOpenedSignature,
    unreadCounts,
    userScope,
  ])

  return {
    // Data
    notice: noticeContent,
    announcements,
    loading,

    // Unread counts
    unreadCount: unreadCounts.total,
    unreadNoticeCount: unreadCounts.notice,
    unreadAnnouncementsCount: unreadCounts.announcements,

    // Popover state
    popoverOpen,
    setPopoverOpen: handlePopoverOpenChange,
    activeTab,
    setActiveTab: handleTabChange,

    // Actions
    openPopover: handleOpenPopover,
    closePopover: () => {
      openedUserScopeRef.current = null
      setPopoverOpen(false)
    },
    closeForToday,
    refetchNotice,
  }
}
