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
import { normalizeInternalRedirect } from './safe-redirect'

describe('normalizeInternalRedirect', () => {
  test('preserves same-origin paths, queries, and fragments', () => {
    assert.equal(
      normalizeInternalRedirect('/dashboard?tab=usage#today'),
      '/dashboard?tab=usage#today'
    )
  })

  for (const target of [
    '//evil.example/path',
    '/\\evil.example/path',
    'https://evil.example/path',
    'javascript:alert(1)',
    'dashboard',
  ]) {
    test(`rejects external or ambiguous target ${target}`, () => {
      assert.equal(normalizeInternalRedirect(target), '/dashboard')
    })
  }
})
