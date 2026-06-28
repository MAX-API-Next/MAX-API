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
import { useQuery } from '@tanstack/react-query'
import { useNotificationStore } from '@/stores/notification-store'
import { getNotice } from '@/lib/api'
import { useStatus } from '@/hooks/use-status'
import {
  getAnnouncementKey,
  getAutoNotificationTab,
  getNotificationContentSignature,
  shouldAutoOpenNotifications,
  type NotificationTab,
} from './notification-utils'

/**
 * Hook to manage notifications (Notice + Announcements)
 * Provides unread counts and read status management
 */
export function useNotifications() {
  const [popoverOpen, setPopoverOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<NotificationTab>('notice')
  const lastAutoOpenedSignatureRef = useRef<string | null>(null)

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
    lastReadNotice,
    markNoticeRead,
    markAnnouncementsRead,
    setClosedUntilDate,
    isAnnouncementRead,
    isNoticeClosed,
  } = useNotificationStore()

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
        return !isAnnouncementRead(key)
      }
    ).length

    return {
      notice: noticeUnread,
      announcements: announcementsUnread,
      total: noticeUnread + announcementsUnread,
    }
  }, [noticeContent, lastReadNotice, announcements, isAnnouncementRead])

  const loading = noticeLoading || statusLoading
  const contentSignature = useMemo(
    () => getNotificationContentSignature(noticeContent, announcements),
    [noticeContent, announcements]
  )

  const markAnnouncementsAsRead = useCallback(() => {
    if (announcements.length > 0) {
      const allKeys = announcements.map((item: Record<string, unknown>) =>
        getAnnouncementKey(item)
      )
      markAnnouncementsRead(allKeys)
    }
  }, [announcements, markAnnouncementsRead])

  const markTabAsRead = useCallback(
    (tab: NotificationTab) => {
      if (tab === 'notice' && noticeContent) {
        markNoticeRead(noticeContent)
      }

      if (tab === 'announcements') {
        markAnnouncementsAsRead()
      }
    },
    [markAnnouncementsAsRead, markNoticeRead, noticeContent]
  )

  // Handle popover open
  const handleOpenPopover = useCallback(
    (tab?: NotificationTab) => {
      const nextTab = tab || activeTab

      markTabAsRead(nextTab)
      setActiveTab(nextTab)
      setPopoverOpen(true)
    },
    [activeTab, markTabAsRead]
  )

  const handlePopoverOpenChange = useCallback(
    (open: boolean) => {
      if (open) {
        handleOpenPopover(activeTab)
        return
      }

      setPopoverOpen(false)
    },
    [activeTab, handleOpenPopover]
  )

  const closeForToday = useCallback(() => {
    setClosedUntilDate(new Date().toDateString())
    setPopoverOpen(false)
  }, [setClosedUntilDate])

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
      !shouldAutoOpenNotifications({
        contentSignature,
        isClosedToday: isNoticeClosed(),
        lastAutoOpenedSignature: lastAutoOpenedSignatureRef.current,
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

    lastAutoOpenedSignatureRef.current = contentSignature
    const timeoutId = window.setTimeout(() => {
      handleOpenPopover(autoTab)
    }, 0)

    return () => {
      window.clearTimeout(timeoutId)
    }
  }, [
    contentSignature,
    announcements.length,
    handleOpenPopover,
    isNoticeClosed,
    loading,
    noticeContent,
    popoverOpen,
    unreadCounts,
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
    closePopover: () => setPopoverOpen(false),
    closeForToday,
    refetchNotice,
  }
}
