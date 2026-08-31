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
import { mutationErrorMessage } from './mutation-error'

describe('mutationErrorMessage', () => {
  test('prefers a non-empty response message', () => {
    assert.equal(
      mutationErrorMessage(
        { response: { data: { message: 'Review conflict' } } },
        'Fallback message'
      ),
      'Review conflict'
    )
  })

  test('falls back when the response message is empty or whitespace', () => {
    assert.equal(
      mutationErrorMessage(
        { response: { data: { message: '' } } },
        'Fallback message'
      ),
      'Fallback message'
    )
    assert.equal(
      mutationErrorMessage(
        { response: { data: { message: '   ' } } },
        'Fallback message'
      ),
      'Fallback message'
    )
  })

  test('falls back when an Error has no visible message', () => {
    assert.equal(
      mutationErrorMessage(new Error(''), 'Fallback message'),
      'Fallback message'
    )
    assert.equal(
      mutationErrorMessage(new Error('   '), 'Fallback message'),
      'Fallback message'
    )
  })
})
