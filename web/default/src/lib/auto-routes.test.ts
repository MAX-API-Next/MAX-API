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
  parseAutoGroupRoutesConfig,
  parseAutoGroupRoutesConfigStrict,
  validateAutoGroupRoutesConfigString,
} from './auto-routes'

describe('auto route config helpers', () => {
  test('keeps legacy auto group arrays compatible', () => {
    const config = parseAutoGroupRoutesConfig('["default","vip","default"]')

    assert.equal(config.default_route, 'auto')
    assert.deepEqual(config.routes, [
      {
        key: 'auto',
        name: 'Auto',
        enabled: true,
        user_selectable: true,
        groups: ['default', 'vip'],
      },
    ])
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
