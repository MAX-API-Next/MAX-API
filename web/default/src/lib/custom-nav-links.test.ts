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
import { keyedTopNavItems } from '@/components/layout/config/top-nav.config'
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

test('saved identity survives reordering same-name groups and removing an earlier child', () => {
  const group = (id: string) => ({
    id,
    title: 'Resources',
    href: '',
    children: [
      { id: id + '-1', title: 'Docs', href: '/docs' },
      { id: id + '-2', title: 'Docs', href: '/docs' },
    ],
  })
  const first = group('first')
  const second = group('second')
  const entries = (items: (typeof first)[]) => {
    const saved = serializeHeaderNavModules(
      parseHeaderNavModules({ custom: { enabled: true, items } })
    )
    return keyedTopNavItems(
      getCustomNavLinks(parseHeaderNavModules(saved).custom)
    )
  }
  const before = entries([first, second])
  const after = entries([
    second,
    { ...first, children: first.children.slice(1) },
  ])
  assert.notEqual(before[0].key, before[1].key)
  assert.deepEqual(
    after.map((entry) => entry.key),
    [before[1].key, before[0].key]
  )
  const original = before[0].link
  const changed = after[1].link
  assert.ok('children' in original && 'children' in changed)
  const children = keyedTopNavItems(original.children)
  assert.notEqual(children[0].key, children[1].key)
  assert.equal(keyedTopNavItems(changed.children)[0].key, children[1].key)
})

test('legacy groups stay keyed while children change and duplicate destinations get unique keys', () => {
  const docs = { title: 'Docs', href: '/docs' }
  const faq = { title: 'FAQ', href: '/faq' }
  const group = { title: 'Resources', children: [docs, faq] }
  const other = { title: 'Resources', children: [faq] }
  assert.equal(
    keyedTopNavItems([group])[0].key,
    keyedTopNavItems([other])[0].key
  )
  const groups = keyedTopNavItems([group, other])
  assert.notEqual(groups[0].key, groups[1].key)
  assert.deepEqual(
    keyedTopNavItems([other, group]).map((x) => x.key),
    [groups[1].key, groups[0].key]
  )
  const keys = (links: (typeof docs)[]) =>
    keyedTopNavItems(links).map((x) => x.key)
  assert.deepEqual(keys([docs, faq, faq]).slice(1), keys([faq, faq]))
  assert.notEqual(keys([faq, faq])[0], keys([faq, faq])[1])
})
