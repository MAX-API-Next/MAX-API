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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { deleteStaleSystemInstances, deleteSystemInstance } from '../api'
import type { SystemInstanceDeleteResponse } from '../types'

export const SYSTEM_INSTANCES_QUERY_KEY = ['system-info', 'instances'] as const

type StaleInstanceCleanupOptions = {
  onDeletedInstance?: () => void
  onDeletedAllStale?: () => void
}

function getDeleteErrorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback
}

export function useStaleInstanceCleanup({
  onDeletedInstance,
  onDeletedAllStale,
}: StaleInstanceCleanupOptions = {}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const invalidateInstances = () =>
    queryClient.invalidateQueries({ queryKey: SYSTEM_INSTANCES_QUERY_KEY })

  const deleteInstanceMutation = useMutation<
    SystemInstanceDeleteResponse,
    unknown,
    string
  >({
    mutationFn: deleteSystemInstance,
    onSuccess: async (res) => {
      if (!res.success) {
        toast.error(t(res.message || 'Delete failed'))
        return
      }

      toast.success(t('Instance deleted'))
      onDeletedInstance?.()
      await invalidateInstances()
    },
    onError: (error) => {
      toast.error(getDeleteErrorMessage(error, t('Delete failed')))
    },
  })

  const deleteAllStaleMutation = useMutation<
    SystemInstanceDeleteResponse,
    unknown,
    void
  >({
    mutationFn: () => deleteStaleSystemInstances(),
    onSuccess: async (res) => {
      if (!res.success) {
        toast.error(t(res.message || 'Delete failed'))
        return
      }

      toast.success(
        t('Deleted {{count}} stale instances', {
          count: res.data?.deleted_count ?? 0,
        })
      )
      onDeletedAllStale?.()
      await invalidateInstances()
    },
    onError: (error) => {
      toast.error(getDeleteErrorMessage(error, t('Delete failed')))
    },
  })

  return {
    deleteAllStale: () => deleteAllStaleMutation.mutate(),
    deleteAllStaleResult: deleteAllStaleMutation.data,
    deleteInstance: deleteInstanceMutation.mutate,
    deleteInstanceResult: deleteInstanceMutation.data,
    deletingAllStale: deleteAllStaleMutation.isPending,
    deletingNodeName: deleteInstanceMutation.isPending
      ? deleteInstanceMutation.variables
      : null,
    isDeletingAnyInstance:
      deleteInstanceMutation.isPending || deleteAllStaleMutation.isPending,
  }
}
