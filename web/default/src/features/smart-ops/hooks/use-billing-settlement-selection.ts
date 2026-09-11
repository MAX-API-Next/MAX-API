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
  canSelectManualTask: boolean
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

export function isManualSettlementSelectable(
  item: BillingSettlementReconciliationItem,
  canSelectManualTask: boolean
): boolean {
  return (
    !item.requires_manual_completion ||
    (item.zero_quota_eligible === true && canSelectManualTask)
  )
}

export function useBillingSettlementSelection(
  params: UseBillingSettlementSelectionParams
): UseBillingSettlementSelectionResult {
  const items = params.items
  const selectedTargets = params.selectedTargets
  const onSelectedTargetsChange = params.onSelectedTargetsChange
  const selectable = useCallback(
    (item: BillingSettlementReconciliationItem): boolean =>
      isManualSettlementSelectable(item, params.canSelectManualTask),
    [params.canSelectManualTask]
  )
  const isSelected = useCallback(
    (item: BillingSettlementReconciliationItem): boolean =>
      selectable(item) &&
      selectedTargets.get(item.id)?.revision === item.revision,
    [selectable, selectedTargets]
  )
  const { allSelected, someSelected } = useMemo(() => {
    return {
      allSelected:
        items.some(selectable) && items.filter(selectable).every(isSelected),
      someSelected: items.some(isSelected),
    }
  }, [isSelected, items, selectable])

  const toggleAll = useCallback(
    (checked: boolean): void => {
      if (!checked) {
        onSelectedTargetsChange(new Map())
        return
      }
      const selectableItems = items.filter(selectable)
      const ordinaryItems = selectableItems.filter(
        (item) => item.zero_quota_eligible !== true
      )
      const zeroItems = selectableItems.filter(
        (item) => item.zero_quota_eligible === true
      )
      const partition = ordinaryItems.length > 0 ? ordinaryItems : zeroItems
      onSelectedTargetsChange(
        new Map(
          partition.map((item) => [
            item.id,
            { id: item.id, revision: item.revision },
          ])
        )
      )
    },
    [items, onSelectedTargetsChange, selectable]
  )

  const toggleItem = useCallback(
    (item: BillingSettlementReconciliationItem, checked: boolean): void => {
      const next = new Map(selectedTargets)
      if (!selectable(item)) {
        next.delete(item.id)
        onSelectedTargetsChange(next)
        return
      }
      if (checked) {
        next.set(item.id, { id: item.id, revision: item.revision })
      } else {
        next.delete(item.id)
      }
      onSelectedTargetsChange(next)
    },
    [onSelectedTargetsChange, selectable, selectedTargets]
  )

  return { allSelected, someSelected, isSelected, toggleAll, toggleItem }
}
