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
import type { QuotaAdjustMode } from '../types'

export function isSafeQuotaValue(value: number) {
  return (
    Number.isSafeInteger(value) && Math.abs(value) <= Number.MAX_SAFE_INTEGER
  )
}

export function isSafeQuotaAdjustment(
  currentQuota: number,
  mode: QuotaAdjustMode,
  value: number
) {
  if (!isSafeQuotaValue(value)) return false
  if (mode === 'override') return value >= 0
  if (!isSafeQuotaValue(currentQuota)) return false
  if (value <= 0) return false

  const result = mode === 'add' ? currentQuota + value : currentQuota - value
  return isSafeQuotaValue(result)
}
