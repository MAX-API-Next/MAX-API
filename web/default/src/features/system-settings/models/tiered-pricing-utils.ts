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
import type { TFunction } from 'i18next'

export function unitCostToPrice(unitCost: number | string): number {
  return Number(unitCost) || 0
}

export function priceToUnitCost(price: number | string): number {
  return Number(price) || 0
}

export function formatTokenHint(
  value: number | string | null | undefined,
  t: TFunction
): string {
  if (value == null || value === '' || Number.isNaN(Number(value))) return ''

  const tokenCount = Number(value)
  if (tokenCount === 0) return '= 0'
  if (tokenCount >= 1_000_000) {
    return t('= {{count}}M tokens', {
      count: (tokenCount / 1_000_000).toLocaleString(),
    })
  }
  if (tokenCount >= 1_000) {
    return t('= {{count}}K tokens', {
      count: (tokenCount / 1_000).toLocaleString(),
    })
  }
  return t('= {{count}} tokens', { count: tokenCount.toLocaleString() })
}
