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
import { test } from 'node:test'
import { getCustomNavLinks, isSupportedNavHref } from './custom-nav-links'
import {
  parseHeaderNavModules,
  serializeHeaderNavModules,
} from './header-nav-modules'

test('external links work in browsers without URL.canParse', () => {
  const descriptor = Object.getOwnPropertyDescriptor(URL, 'canParse')
  Reflect.deleteProperty(URL, 'canParse')
  try {
    assert.equal(isSupportedNavHref('https://shop.example/products'), true)
    assert.equal(isSupportedNavHref('http://'), false)
    assert.equal(isSupportedNavHref('ftp://shop.example'), false)
  } finally {
    if (descriptor) Object.defineProperty(URL, 'canParse', descriptor)
  }
})

test('legacy links survive editing and saving, including while hidden', () => {
  const config = parseHeaderNavModules(
    JSON.stringify({
      custom: {
        enabled: false,
        title: ' Shop ',
        href: ' https://shop.example/ ',
      },
    })
  )
  assert.deepEqual(config.custom.items, [
    { title: 'Shop', href: 'https://shop.example/', children: [] },
  ])
  assert.deepEqual(getCustomNavLinks(config.custom), [])
  assert.deepEqual(
    parseHeaderNavModules(serializeHeaderNavModules(config)),
    config
  )
  assert.deepEqual(getCustomNavLinks({ ...config.custom, enabled: true }), [
    {
      title: 'Shop',
      href: 'https://shop.example/',
      external: true,
      literalTitle: true,
    },
  ])
  assert.deepEqual(
    parseHeaderNavModules({
      custom: { enabled: true, title: 'Shop', href: '/shop', items: [] },
    }).custom.items,
    []
  )
})

test('groups keep their optional landing link and ordered children; invalid entries are skipped', () => {
  const config = parseHeaderNavModules({
    custom: {
      enabled: true,
      items: [
        { title: 'About', href: '/about' },
        {
          title: 'Resources',
          href: '/docs',
          children: [
            { title: 'Updates', href: 'https://example.org/updates' },
            null,
            { title: 'Invalid', href: '//example.org' },
            { title: 'FAQ', href: '/faq?tab=help#new' },
          ],
        },
        { title: 'Community', children: [{ title: 'Forum', href: '/forum' }] },
        { title: 'Empty', children: [{ title: '', href: '/empty' }] },
        { title: 'Invalid', href: 'javascript:void(0)' },
      ],
    },
  })
  assert.deepEqual(
    getCustomNavLinks(
      parseHeaderNavModules(serializeHeaderNavModules(config)).custom
    ),
    [
      { title: 'About', href: '/about', external: false, literalTitle: true },
      {
        title: 'Resources',
        children: [
          {
            title: 'Resources',
            href: '/docs',
            external: false,
            literalTitle: true,
          },
          {
            title: 'Updates',
            href: 'https://example.org/updates',
            external: true,
            literalTitle: true,
          },
          {
            title: 'FAQ',
            href: '/faq?tab=help#new',
            external: false,
            literalTitle: true,
          },
        ],
      },
      {
        title: 'Community',
        children: [
          {
            title: 'Forum',
            href: '/forum',
            external: false,
            literalTitle: true,
          },
        ],
      },
    ]
  )
})
