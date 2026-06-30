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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  getAnnouncementKey,
  getAutoNotificationTab,
  getNotificationContentSignature,
  getScopedNotificationAutoOpenSignature,
  shouldAutoOpenNotifications,
  shouldRememberOpenNotificationSignature,
} from './notification-utils'

describe('notification auto open helpers', () => {
  test('keeps announcement keys stable when an existing announcement is edited', () => {
    const originalKey = getAnnouncementKey({
      id: 10,
      content: 'Initial announcement',
      publishDate: '2026-06-28T08:00:00Z',
    })
    const editedKey = getAnnouncementKey({
      id: 10,
      content: 'Edited announcement',
      publishDate: '2026-06-28T08:00:00Z',
    })

    assert.equal(originalKey, 'id:10')
    assert.equal(editedKey, originalKey)
  })

  test('keeps content-hash keys for announcements without ids', () => {
    const originalKey = getAnnouncementKey({
      content: 'Initial announcement',
      publishDate: '2026-06-28T08:00:00Z',
    })
    const editedKey = getAnnouncementKey({
      content: 'Edited announcement',
      publishDate: '2026-06-28T08:00:00Z',
    })

    assert.notEqual(originalKey, editedKey)
    assert.match(originalKey, /^hash:/)
    assert.match(editedKey, /^hash:/)
  })

  test('keeps content signatures stable when announcement order changes', () => {
    const firstSignature = getNotificationContentSignature('', [
      { id: 2, content: 'Second' },
      { id: 1, content: 'First' },
    ])
    const secondSignature = getNotificationContentSignature('', [
      { id: 1, content: 'First' },
      { id: 2, content: 'Second' },
    ])

    assert.equal(firstSignature, secondSignature)
  })

  test('opens unread notice content after notifications finish loading', () => {
    const unreadCounts = {
      notice: 1,
      announcements: 0,
      total: 1,
    }

    assert.equal(
      getAutoNotificationTab({
        hasAnnouncements: false,
        hasNotice: true,
        unreadCounts,
      }),
      'notice'
    )
    assert.equal(
      shouldAutoOpenNotifications({
        autoOpenedSignatures: [],
        contentSignature: getNotificationContentSignature('new notice', []),
        isClosedToday: false,
        loading: false,
        popoverOpen: false,
      }),
      true
    )
  })

  test('opens unread announcements when no notice is unread', () => {
    const unreadCounts = {
      notice: 0,
      announcements: 1,
      total: 1,
    }

    assert.equal(
      getAutoNotificationTab({
        hasAnnouncements: true,
        hasNotice: false,
        unreadCounts,
      }),
      'announcements'
    )
    assert.equal(
      shouldAutoOpenNotifications({
        autoOpenedSignatures: [],
        contentSignature: getNotificationContentSignature('', [
          { id: 10, content: 'new timeline item' },
        ]),
        isClosedToday: false,
        loading: false,
        popoverOpen: false,
      }),
      true
    )
  })

  test('opens existing read notice content once per page session', () => {
    const unreadCounts = {
      notice: 0,
      announcements: 0,
      total: 0,
    }

    assert.equal(
      getAutoNotificationTab({
        hasAnnouncements: false,
        hasNotice: true,
        unreadCounts,
      }),
      'notice'
    )
    assert.equal(
      shouldAutoOpenNotifications({
        autoOpenedSignatures: [],
        contentSignature: getNotificationContentSignature('existing notice', []),
        isClosedToday: false,
        loading: false,
        popoverOpen: false,
      }),
      true
    )
  })

  test('does not auto open after closing for today', () => {
    assert.equal(
      shouldAutoOpenNotifications({
        autoOpenedSignatures: [],
        contentSignature: getNotificationContentSignature('new notice', []),
        isClosedToday: true,
        loading: false,
        popoverOpen: false,
      }),
      false
    )
  })

  test('does not repeatedly auto open the same notification content', () => {
    const contentSignature = getScopedNotificationAutoOpenSignature(
      1,
      getNotificationContentSignature('new notice', [])
    )

    assert.equal(
      shouldAutoOpenNotifications({
        autoOpenedSignatures: [contentSignature],
        contentSignature,
        isClosedToday: false,
        loading: false,
        popoverOpen: false,
      }),
      false
    )
  })

  test('scopes auto-open signatures by user', () => {
    const contentSignature = getNotificationContentSignature('existing notice', [])
    const userOneSignature = getScopedNotificationAutoOpenSignature(
      1,
      contentSignature
    )
    const userTwoSignature = getScopedNotificationAutoOpenSignature(
      2,
      contentSignature
    )

    assert.notEqual(userOneSignature, userTwoSignature)
    assert.equal(
      shouldAutoOpenNotifications({
        autoOpenedSignatures: [userOneSignature],
        contentSignature: userTwoSignature,
        isClosedToday: false,
        loading: false,
        popoverOpen: false,
      }),
      true
    )
  })

  test('does not auto open content already seen while the popover was open', () => {
    const contentSignature = getScopedNotificationAutoOpenSignature(
      1,
      getNotificationContentSignature('manually opened notice', [])
    )

    assert.equal(
      shouldAutoOpenNotifications({
        autoOpenedSignatures: [contentSignature],
        contentSignature,
        isClosedToday: false,
        loading: false,
        popoverOpen: false,
      }),
      false
    )
  })

  test('remembers content that arrives while the same user has the popover open', () => {
    const contentSignature = getScopedNotificationAutoOpenSignature(
      1,
      getNotificationContentSignature('loaded while open', [])
    )

    assert.equal(
      shouldRememberOpenNotificationSignature({
        contentSignature,
        openedUserScope: 'user:1',
        userScope: 'user:1',
      }),
      true
    )
    assert.equal(
      shouldAutoOpenNotifications({
        autoOpenedSignatures: [contentSignature],
        contentSignature,
        isClosedToday: false,
        loading: false,
        popoverOpen: false,
      }),
      false
    )
  })

  test('does not remember loaded content for a different user scope', () => {
    const contentSignature = getScopedNotificationAutoOpenSignature(
      2,
      getNotificationContentSignature('loaded after auth switch', [])
    )

    assert.equal(
      shouldRememberOpenNotificationSignature({
        contentSignature,
        openedUserScope: 'user:1',
        userScope: 'user:2',
      }),
      false
    )
  })
})
