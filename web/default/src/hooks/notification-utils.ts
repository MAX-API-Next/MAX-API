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
export type NotificationTab = 'notice' | 'announcements'

export interface NotificationUnreadCounts {
  announcements: number
  notice: number | string
  total: number
}

interface ShouldAutoOpenNotificationsOptions {
  contentSignature: string
  isClosedToday: boolean
  lastAutoOpenedSignature: string | null
  loading: boolean
  popoverOpen: boolean
}

interface GetAutoNotificationTabOptions {
  hasAnnouncements: boolean
  hasNotice: boolean
  unreadCounts: NotificationUnreadCounts
}

function hashString(input: string): string {
  let hash = 0
  if (!input) return '0'

  for (let i = 0; i < input.length; i += 1) {
    const chr = input.charCodeAt(i)
    hash = (hash << 5) - hash + chr
    hash |= 0
  }

  return hash.toString(36)
}

function getAnnouncementFingerprint(item: Record<string, unknown>): string {
  return JSON.stringify({
    publishDate: (item?.publishDate as string) || '',
    content: ((item?.content as string) || '').trim(),
    extra: ((item?.extra as string) || '').trim(),
    type: (item?.type as string) || '',
    title: ((item?.title as string) || '').trim(),
    link: ((item?.link as string) || '').trim(),
  })
}

/**
 * Generate a unique key for an announcement.
 * Prefer backend id but include a content version so edits register.
 */
export function getAnnouncementKey(item: Record<string, unknown>): string {
  if (!item) return ''

  const version = hashString(getAnnouncementFingerprint(item))

  if (item.id !== undefined && item.id !== null) {
    return `id:${item.id}:hash:${version}`
  }

  return `hash:${version}`
}

export function getNotificationContentSignature(
  noticeContent: string,
  announcements: Record<string, unknown>[]
): string {
  const notice = noticeContent.trim()
  const announcementKeys = announcements
    .map((item: Record<string, unknown>) => getAnnouncementKey(item))
    .filter(Boolean)
    .sort()

  if (!notice && announcementKeys.length === 0) {
    return ''
  }

  return JSON.stringify({
    notice,
    announcements: announcementKeys,
  })
}

export function getAutoNotificationTab(
  options: GetAutoNotificationTabOptions
): NotificationTab | null {
  if (Number(options.unreadCounts.notice) > 0) {
    return 'notice'
  }

  if (options.unreadCounts.announcements > 0) {
    return 'announcements'
  }

  if (options.hasNotice) {
    return 'notice'
  }

  if (options.hasAnnouncements) {
    return 'announcements'
  }

  return null
}

export function shouldAutoOpenNotifications(
  options: ShouldAutoOpenNotificationsOptions
): boolean {
  return (
    !options.loading &&
    !options.popoverOpen &&
    !options.isClosedToday &&
    options.contentSignature !== '' &&
    options.contentSignature !== options.lastAutoOpenedSignature
  )
}
