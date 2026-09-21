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
import { z } from 'zod'
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

const batchStatusResultSchema = z.object({
  id: z.number().int().positive(),
  outcome: z.enum(['updated', 'unchanged', 'rejected', 'unknown']),
  code: z.enum(['forbidden', 'not_found', 'conflict', 'unknown']).optional(),
  status: z
    .union([z.literal(USER_STATUS.ENABLED), z.literal(USER_STATUS.DISABLED)])
    .optional(),
})

// An incomplete, duplicated or contradictory response must never imply success.
// Results are matched by account ID, not response order.
export function normalizeBatchStatusResults(
  ids: number[],
  results: unknown,
  targetStatus: typeof USER_STATUS.ENABLED | typeof USER_STATUS.DISABLED
): BatchUserStatusResult[] {
  const entries: unknown[] = Array.isArray(results) ? results : []
  return ids.map((id) => {
    // Count duplicates before validation, including malformed duplicate entries.
    const matches = entries.filter(
      (entry) =>
        typeof entry === 'object' &&
        entry !== null &&
        'id' in entry &&
        entry.id === id
    )
    const parsed = batchStatusResultSchema.safeParse(matches[0])
    if (matches.length !== 1 || !parsed.success) {
      return { id, outcome: 'unknown', code: 'unknown' }
    }
    const result = parsed.data
    if (
      (result.outcome === 'updated' || result.outcome === 'unchanged') &&
      (result.status !== targetStatus || result.code !== undefined)
    ) {
      return { id, outcome: 'unknown', code: 'unknown' }
    }
    return result
  })
}
