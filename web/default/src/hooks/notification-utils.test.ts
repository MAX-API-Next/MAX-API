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
  getLastAutoOpenedNotificationSignature,
  getNotificationContentSignature,
  rememberAutoOpenedNotificationSignature,
  shouldAutoOpenNotifications,
} from './notification-utils'

describe('notification auto open helpers', () => {
  test('treats edited announcements with the same id as new keys', () => {
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

    assert.notEqual(originalKey, editedKey)
    assert.match(originalKey, /^id:10:hash:/)
    assert.match(editedKey, /^id:10:hash:/)
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
        contentSignature: getNotificationContentSignature('new notice', []),
        isClosedToday: false,
        lastAutoOpenedSignature: null,
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
        contentSignature: getNotificationContentSignature('', [
          { id: 10, content: 'new timeline item' },
        ]),
        isClosedToday: false,
        lastAutoOpenedSignature: null,
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
        contentSignature: getNotificationContentSignature('existing notice', []),
        isClosedToday: false,
        lastAutoOpenedSignature: null,
        loading: false,
        popoverOpen: false,
      }),
      true
    )
  })

  test('does not auto open after closing for today', () => {
    assert.equal(
      shouldAutoOpenNotifications({
        contentSignature: getNotificationContentSignature('new notice', []),
        isClosedToday: true,
        lastAutoOpenedSignature: null,
        loading: false,
        popoverOpen: false,
      }),
      false
    )
  })

  test('does not repeatedly auto open the same notification content', () => {
    const contentSignature = getNotificationContentSignature('new notice', [])

    assert.equal(
      shouldAutoOpenNotifications({
        contentSignature,
        isClosedToday: false,
        lastAutoOpenedSignature: contentSignature,
        loading: false,
        popoverOpen: false,
      }),
      false
    )
  })

  test('remembers auto opened content across hook remounts in the same page session', () => {
    const contentSignature = getNotificationContentSignature('existing notice', [])

    assert.equal(rememberAutoOpenedNotificationSignature(contentSignature), true)
    assert.equal(rememberAutoOpenedNotificationSignature(contentSignature), false)
  })

  test('does not auto open content already seen while the popover was open', () => {
    const contentSignature = getNotificationContentSignature(
      'manually opened notice',
      []
    )

    assert.equal(rememberAutoOpenedNotificationSignature(contentSignature), true)
    assert.equal(
      shouldAutoOpenNotifications({
        contentSignature,
        isClosedToday: false,
        lastAutoOpenedSignature: getLastAutoOpenedNotificationSignature(),
        loading: false,
        popoverOpen: false,
      }),
      false
    )
  })

  test('uses memory fallback when session storage writes fail', () => {
    const windowDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'window')
    const contentSignature = getNotificationContentSignature(
      'restricted storage notice',
      []
    )

    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      value: {
        sessionStorage: {
          getItem: () => null,
          setItem: () => {
            throw new Error('storage disabled')
          },
        },
      },
    })

    try {
      assert.equal(rememberAutoOpenedNotificationSignature(contentSignature), true)
      assert.equal(getLastAutoOpenedNotificationSignature(), contentSignature)
      assert.equal(rememberAutoOpenedNotificationSignature(contentSignature), false)
    } finally {
      if (windowDescriptor) {
        Object.defineProperty(globalThis, 'window', windowDescriptor)
      } else {
        delete (globalThis as { window?: unknown }).window
      }
    }
  })
})
