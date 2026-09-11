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
  return !item.requires_manual_completion || canSelectManualTask
}

export function getBillingSettlementSelectionPartition(
  items: BillingSettlementReconciliationItem[],
  canSelectManualTask: boolean
): BillingSettlementReconciliationItem[] {
  const selectableItems = items.filter((item) =>
    isManualSettlementSelectable(item, canSelectManualTask)
  )
  const ordinaryItems = selectableItems.filter(
    (item) => !item.requires_manual_completion
  )
  return ordinaryItems.length > 0
    ? ordinaryItems
    : selectableItems.filter((item) => item.requires_manual_completion)
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
  const selectionPartition = useMemo(
    () =>
      getBillingSettlementSelectionPartition(items, params.canSelectManualTask),
    [items, params.canSelectManualTask]
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
        selectionPartition.length > 0 && selectionPartition.every(isSelected),
      someSelected: items.some(isSelected),
    }
  }, [isSelected, items, selectionPartition])

  const toggleAll = useCallback(
    (checked: boolean): void => {
      if (!checked) {
        onSelectedTargetsChange(new Map())
        return
      }
      onSelectedTargetsChange(
        new Map(
          selectionPartition.map((item) => [
            item.id,
            { id: item.id, revision: item.revision },
          ])
        )
      )
    },
    [onSelectedTargetsChange, selectionPartition]
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
