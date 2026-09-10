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
import { QueryClient } from '@tanstack/react-query'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { invalidateQueriesAfterOptionUpdate } from './use-update-option'

test('invalidates cached model pricing after task rate cards are saved', () => {
  const queryClient = new QueryClient()
  queryClient.setQueryData(['system-options'], { data: 'options' })
  queryClient.setQueryData(['pricing'], { data: 'old-price' })
  queryClient.setQueryData(['status'], { data: 'status' })

  invalidateQueriesAfterOptionUpdate(
    queryClient,
    'task_billing_setting.rate_cards'
  )

  assert.equal(
    queryClient.getQueryState(['system-options'])?.isInvalidated,
    true
  )
  assert.equal(queryClient.getQueryState(['pricing'])?.isInvalidated, true)
  assert.equal(queryClient.getQueryState(['status'])?.isInvalidated, false)
  queryClient.clear()
})

test('does not invalidate cached model pricing for unrelated settings', () => {
  const queryClient = new QueryClient()
  queryClient.setQueryData(['pricing'], { data: 'current-price' })

  invalidateQueriesAfterOptionUpdate(queryClient, 'Notice')

  assert.equal(queryClient.getQueryState(['pricing'])?.isInvalidated, false)
  queryClient.clear()
})
