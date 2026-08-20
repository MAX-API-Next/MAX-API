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
import type { TopupStatus } from '../types'
import { getStatusConfig, isCompletableTopupStatus } from './billing'

describe('top-up billing status', () => {
  test('shows paid reconciliation orders explicitly', () => {
    assert.deepEqual(getStatusConfig('paid_reconciliation' as TopupStatus), {
      variant: 'warning',
      label: 'Paid - Needs Reconciliation',
    })
  })

  test('allows admins to complete pending and reconciliation orders only', () => {
    assert.equal(isCompletableTopupStatus('pending'), true)
    assert.equal(isCompletableTopupStatus('paid_reconciliation'), true)
    assert.equal(isCompletableTopupStatus('success'), false)
    assert.equal(isCompletableTopupStatus('failed'), false)
    assert.equal(isCompletableTopupStatus('expired'), false)
  })
})
