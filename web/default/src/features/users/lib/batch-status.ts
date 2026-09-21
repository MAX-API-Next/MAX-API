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
import type { AuthUser } from '@/stores/auth-store'
import { USER_ROLE, USER_STATUS, isUserDeleted } from '../constants'
import type { BatchUserStatusResult, User } from '../types'

export const MAX_USER_STATUS_BATCH_SIZE = 100

export function canBatchChangeUserStatus(
  user: User,
  actor: Pick<AuthUser, 'id' | 'role'> | null
): boolean {
  return Boolean(
    actor &&
    actor.role >= USER_ROLE.ADMIN &&
    actor.id !== user.id &&
    actor.role > user.role &&
    user.role < USER_ROLE.ROOT &&
    !isUserDeleted(user) &&
    (user.status === USER_STATUS.ENABLED ||
      user.status === USER_STATUS.DISABLED)
  )
}

// An incomplete, duplicated or unrecognized response must never imply success.
// Results are matched by account ID, not response order.
export function normalizeBatchStatusResults(
  ids: number[],
  results: BatchUserStatusResult[] | undefined
): BatchUserStatusResult[] {
  const allowed = new Set(['updated', 'unchanged', 'rejected', 'unknown'])
  return ids.map((id) => {
    const matches = Array.isArray(results)
      ? results.filter((result) => result?.id === id)
      : []
    if (matches.length !== 1 || !allowed.has(matches[0].outcome)) {
      return { id, outcome: 'unknown', code: 'unknown' }
    }
    return matches[0]
  })
}
