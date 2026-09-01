/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import { useCallback, useMemo } from 'react'
import type {
  BillingSettlementReconciliationItem,
  BillingSettlementReviewTarget,
} from '../types'

interface UseBillingSettlementSelectionParams {
  items: BillingSettlementReconciliationItem[]
  selectedTargets: ReadonlyMap<number, BillingSettlementReviewTarget>
  onSelectedTargetsChange: (
    targets: Map<number, BillingSettlementReviewTarget>
  ) => void
}

interface UseBillingSettlementSelectionResult {
  allSelected: boolean
  someSelected: boolean
  isSelected: (item: BillingSettlementReconciliationItem) => boolean
  toggleAll: (checked: boolean) => void
  toggleItem: (
    item: BillingSettlementReconciliationItem,
    checked: boolean
  ) => void
}

export function useBillingSettlementSelection(
  params: UseBillingSettlementSelectionParams
): UseBillingSettlementSelectionResult {
  const items = params.items
  const selectedTargets = params.selectedTargets
  const onSelectedTargetsChange = params.onSelectedTargetsChange
  const isSelected = useCallback(
    (item: BillingSettlementReconciliationItem): boolean =>
      selectedTargets.get(item.id)?.revision === item.revision,
    [selectedTargets]
  )
  const { allSelected, someSelected } = useMemo(() => {
    return {
      allSelected: items.length > 0 && items.every((item) => isSelected(item)),
      someSelected: items.some((item) => isSelected(item)),
    }
  }, [isSelected, items])

  const toggleAll = useCallback(
    (checked: boolean): void => {
      onSelectedTargetsChange(
        checked
          ? new Map(
              items.map((item) => [
                item.id,
                { id: item.id, revision: item.revision },
              ])
            )
          : new Map()
      )
    },
    [items, onSelectedTargetsChange]
  )

  const toggleItem = useCallback(
    (item: BillingSettlementReconciliationItem, checked: boolean): void => {
      const next = new Map(selectedTargets)
      if (checked) {
        next.set(item.id, { id: item.id, revision: item.revision })
      } else {
        next.delete(item.id)
      }
      onSelectedTargetsChange(next)
    },
    [onSelectedTargetsChange, selectedTargets]
  )

  return { allSelected, someSelected, isSelected, toggleAll, toggleItem }
}
