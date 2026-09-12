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
import {
  useCallback,
  useState,
  type Dispatch,
  type SetStateAction,
} from 'react'
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import {
  completeManualTaskBillingSettlements,
  completeManualTaskBillingSettlementsZero,
} from '../api'
import { mutationErrorMessage } from '../lib/mutation-error'
import {
  SMART_OPS_ACTIVE_ALERTS_QUERY_KEY,
  SMART_OPS_BILLING_RECONCILIATION_QUERY_KEY,
} from '../lib/query-keys'
import type {
  BillingSettlementReconciliationItem,
  BillingSettlementReviewTarget,
  ManualTaskBillingBatchCompletionData,
  ManualTaskBillingBatchFailure,
} from '../types'

interface UseManualTaskBatchSettlementParams {
  onSelectionCleared: () => void
}

interface UseManualTaskBatchSettlementResult {
  manualTaskBatchItems: BillingSettlementReconciliationItem[]
  setManualTaskBatchItems: (
    items: BillingSettlementReconciliationItem[]
  ) => void
  batchFailures: ManualTaskBillingBatchFailure[]
  setBatchFailures: Dispatch<SetStateAction<ManualTaskBillingBatchFailure[]>>
  zeroSettlementMutation: UseMutationResult<
    ManualTaskBillingBatchCompletionData | undefined,
    Error,
    BillingSettlementReviewTarget[],
    unknown
  >
  manualTaskBatchCompletionMutation: UseMutationResult<
    ManualTaskBillingBatchCompletionData | undefined,
    Error,
    {
      items: BillingSettlementReconciliationItem[]
      actualQuotas: Record<number, number>
    },
    unknown
  >
}

export function mergeManualTaskBatchFailures(
  current: ManualTaskBillingBatchFailure[],
  data?: ManualTaskBillingBatchCompletionData
): ManualTaskBillingBatchFailure[] {
  if (!data) return current

  const completedIDs = new Set(data.settlement_ids)
  const next = current.filter(
    (failure) => !completedIDs.has(failure.settlement_id)
  )
  const indexes = new Map(
    next.map((failure, index) => [failure.settlement_id, index])
  )
  for (const failure of data.failed) {
    const index = indexes.get(failure.settlement_id)
    if (index === undefined) {
      indexes.set(failure.settlement_id, next.length)
      next.push(failure)
    } else {
      next[index] = failure
    }
  }
  return next
}

export function useManualTaskBatchSettlement(
  params: UseManualTaskBatchSettlementParams
): UseManualTaskBatchSettlementResult {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [manualTaskBatchItems, setManualTaskBatchItems] = useState<
    BillingSettlementReconciliationItem[]
  >([])
  const [batchFailures, setBatchFailures] = useState<
    ManualTaskBillingBatchFailure[]
  >([])

  const invalidateSettlementQueries = useCallback(async (): Promise<void> => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: SMART_OPS_ACTIVE_ALERTS_QUERY_KEY,
      }),
      queryClient.invalidateQueries({
        queryKey: SMART_OPS_BILLING_RECONCILIATION_QUERY_KEY,
      }),
    ])
  }, [queryClient])

  const zeroSettlementMutation = useMutation({
    mutationKey: ['smart-ops', 'manual-task-billing-zero-batch'],
    mutationFn: async (
      targets: BillingSettlementReviewTarget[]
    ): Promise<ManualTaskBillingBatchCompletionData | undefined> => {
      const response = await completeManualTaskBillingSettlementsZero({
        items: targets,
      })
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to complete manual task billing.')
        )
      }
      return response.data
    },
    onSuccess: (data) => {
      params.onSelectionCleared()
      const completed = data?.completed_count ?? 0
      const failed = data?.failed_count ?? 0
      const failures = data?.failed ?? []
      setBatchFailures((current) => mergeManualTaskBatchFailures(current, data))
      if (completed === 0) {
        toast.error(t('No selected task settlements were completed.'))
      } else if (failed > 0) {
        toast.warning(
          t(
            'Completed {{completed}} selected task settlements; {{failed}} remain for review.',
            { completed, failed }
          )
        )
      } else {
        toast.success(
          t(
            'Completed selected task settlements with zero final quota: {{count}}.',
            { count: completed }
          )
        )
      }
      if (failures.length > 0) {
        toast.warning(
          t('Some settlements remain for review: {{ids}}', {
            ids: failures
              .map((failure) => `#${failure.settlement_id}`)
              .join(', '),
          })
        )
      }
    },
    onSettled: invalidateSettlementQueries,
    onError: (error) => {
      handleServerError(error, {
        fallback: mutationErrorMessage(
          error,
          t('Failed to complete manual task billing.')
        ),
      })
    },
  })

  const manualTaskBatchCompletionMutation = useMutation({
    mutationKey: ['smart-ops', 'manual-task-billing-exact-batch'],
    mutationFn: async ({
      items,
      actualQuotas,
    }: {
      items: BillingSettlementReconciliationItem[]
      actualQuotas: Record<number, number>
    }): Promise<ManualTaskBillingBatchCompletionData | undefined> => {
      const response = await completeManualTaskBillingSettlements({
        items: items.map((item) => ({
          id: item.id,
          revision: item.revision,
          actual_quota: actualQuotas[item.id],
        })),
      })
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to complete manual task billing.')
        )
      }
      return response.data
    },
    onSuccess: (data) => {
      setManualTaskBatchItems([])
      params.onSelectionCleared()
      const completed = data?.completed_count ?? 0
      const failed = data?.failed_count ?? 0
      setBatchFailures((current) => mergeManualTaskBatchFailures(current, data))
      if (completed === 0) {
        toast.error(t('No selected task settlements were completed.'))
      } else if (failed > 0) {
        toast.warning(
          t(
            'Completed {{completed}} selected task settlements; {{failed}} remain for review.',
            { completed, failed }
          )
        )
      } else {
        toast.success(
          t('Completed selected task settlements: {{count}}.', {
            count: completed,
          })
        )
      }
    },
    onSettled: invalidateSettlementQueries,
    onError: (error) => {
      handleServerError(error, {
        fallback: mutationErrorMessage(
          error,
          t('Failed to complete manual task billing.')
        ),
      })
    },
  })

  return {
    manualTaskBatchItems,
    setManualTaskBatchItems,
    batchFailures,
    setBatchFailures,
    zeroSettlementMutation,
    manualTaskBatchCompletionMutation,
  }
}
