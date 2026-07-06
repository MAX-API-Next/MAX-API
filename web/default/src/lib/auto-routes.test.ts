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
  getDefaultAutoRouteGroups,
  parseAutoGroupRoutesConfig,
  parseAutoGroupRoutesConfigStrict,
  stringifyAutoGroupRoutesConfig,
  validateAutoGroupRoutesConfigString,
} from './auto-routes'

describe('auto route config helpers', () => {
  test('keeps legacy auto group arrays compatible', () => {
    const config = parseAutoGroupRoutesConfig('["default","vip","default"]')

    assert.equal(config.default_route, 'auto')
    assert.deepEqual(config.routes, [
      {
        key: 'auto',
        enabled: true,
        user_selectable: true,
        groups: ['default', 'vip'],
      },
    ])

    const serialized = JSON.parse(stringifyAutoGroupRoutesConfig(config))
    assert.equal('name' in serialized.routes[0], false)
  })

  test('rejects nested auto routes in strict save validation', () => {
    const raw = JSON.stringify({
      version: 1,
      default_route: 'auto',
      routes: [
        {
          key: 'auto',
          enabled: true,
          user_selectable: true,
          groups: ['default', 'auto:fast'],
        },
      ],
    })

    assert.throws(
      () => parseAutoGroupRoutesConfigStrict(raw),
      /real groups only/
    )
    assert.equal(validateAutoGroupRoutesConfigString(raw).valid, false)
  })

  test('normalizes non-string route names without dropping the route config', () => {
    const config = parseAutoGroupRoutesConfig(
      JSON.stringify({
        version: 1,
        default_route: 'auto:fast',
        routes: [
          {
            key: 'auto:fast',
            name: 123,
            enabled: true,
            user_selectable: true,
            groups: ['vip'],
          },
        ],
      })
    )

    assert.equal(config.default_route, 'auto:fast')
    assert.deepEqual(config.routes, [
      {
        key: 'auto:fast',
        name: 'auto:fast',
        enabled: true,
        user_selectable: true,
        groups: ['vip'],
      },
    ])
  })

  test('keeps disabled default route available in lenient config reads', () => {
    const config = parseAutoGroupRoutesConfig(
      JSON.stringify({
        version: 1,
        default_route: 'auto:fast',
        routes: [
          {
            key: 'auto',
            enabled: true,
            user_selectable: true,
            groups: ['default'],
          },
          {
            key: 'auto:fast',
            enabled: false,
            user_selectable: true,
            groups: ['vip', 'vip'],
          },
        ],
      })
    )

    assert.equal(config.default_route, 'auto:fast')
    assert.deepEqual(config.routes, [
      {
        key: 'auto',
        enabled: true,
        user_selectable: true,
        groups: ['default'],
      },
      {
        key: 'auto:fast',
        name: 'auto:fast',
        enabled: false,
        user_selectable: true,
        groups: ['vip'],
      },
    ])
    assert.deepEqual(getDefaultAutoRouteGroups(config), ['vip'])
  })

  test('rejects disabled default route in strict save validation', () => {
    const raw = JSON.stringify({
      version: 1,
      default_route: 'auto:fast',
      routes: [
        {
          key: 'auto',
          enabled: true,
          user_selectable: true,
          groups: ['default'],
        },
        {
          key: 'auto:fast',
          enabled: false,
          user_selectable: true,
          groups: ['vip'],
        },
      ],
    })

    assert.throws(
      () => parseAutoGroupRoutesConfigStrict(raw),
      /must be enabled/
    )
    assert.equal(validateAutoGroupRoutesConfigString(raw).valid, false)
  })
})
