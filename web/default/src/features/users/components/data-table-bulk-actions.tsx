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
import { useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { batchManageUserStatus } from '../api'
import { USER_STATUS } from '../constants'
import {
  canBatchChangeUserStatus,
  MAX_USER_STATUS_BATCH_SIZE,
  normalizeBatchStatusResults,
} from '../lib/batch-status'
import type {
  BatchUserStatusAction,
  BatchUserStatusResult,
  User,
} from '../types'
import { UserStatusBatchDialog } from './user-status-batch-dialog'

interface DataTableBulkActionsProps {
  table: Table<User>
  selectionScope: string
  isFetching: boolean
}

export function DataTableBulkActions(props: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const actor = useAuthStore((state) => state.auth.user)
  const queryClient = useQueryClient()
  const [batch, setBatch] = useState<{
    action: BatchUserStatusAction
    users: User[]
    scope: string
  } | null>(null)
  const [results, setResults] = useState<BatchUserStatusResult[] | null>(null)
  const submitting = useRef(false)
  const mutation = useMutation({
    mutationFn: (selected: NonNullable<typeof batch>) =>
      batchManageUserStatus(
        selected.action,
        selected.users.map(({ id, status }) => ({ id, status }))
      ),
    retry: false,
    // The API interceptor already shows HTTP/business errors. Override the
    // default mutation toast; the catch below still preserves unknown results.
    onError: () => undefined,
  })
  const selected = props.table
    .getFilteredSelectedRowModel()
    .rows.map((row) => row.original)
  const canSubmit =
    selected.length > 0 &&
    selected.length <= MAX_USER_STATUS_BATCH_SIZE &&
    selected.every((user) => canBatchChangeUserStatus(user, actor)) &&
    !props.isFetching

  const openBatch = (action: BatchUserStatusAction) => {
    if (!canSubmit || submitting.current) return
    setResults(null)
    setBatch({
      action,
      users: selected.map((user) => ({ ...user })),
      scope: props.selectionScope,
    })
  }
  const stale = batch !== null && batch.scope !== props.selectionScope
  const confirm = async () => {
    if (
      !batch ||
      submitting.current ||
      results ||
      stale ||
      props.isFetching ||
      !batch.users.every((user) => canBatchChangeUserStatus(user, actor))
    )
      return
    submitting.current = true
    const targetStatus =
      batch.action === 'enable' ? USER_STATUS.ENABLED : USER_STATUS.DISABLED
    try {
      const response = await mutation.mutateAsync(batch)
      setResults(
        normalizeBatchStatusResults(
          batch.users.map((user) => user.id),
          response.success ? response.data?.results : undefined,
          targetStatus
        )
      )
    } catch {
      // The server may have committed some items before the connection failed.
      // Never auto-retry and never label these accounts as confirmed failures.
      setResults(
        normalizeBatchStatusResults(
          batch.users.map((user) => user.id),
          undefined,
          targetStatus
        )
      )
    } finally {
      submitting.current = false
      props.table.resetRowSelection()
      void queryClient.invalidateQueries({ queryKey: ['users'] })
    }
  }

  return (
    <>
      <BulkActionsToolbar
        table={props.table}
        entityName={t('User')}
        entityNamePlural={t('Users')}
      >
        <Button
          size='sm'
          variant='outline'
          title={t('Select at most {{count}} users per batch.', {
            count: MAX_USER_STATUS_BATCH_SIZE,
          })}
          disabled={!canSubmit || mutation.isPending}
          onClick={() => openBatch('disable')}
        >
          {t('Batch disable')}
        </Button>
        <Button
          size='sm'
          variant='outline'
          title={t('Select at most {{count}} users per batch.', {
            count: MAX_USER_STATUS_BATCH_SIZE,
          })}
          disabled={!canSubmit || mutation.isPending}
          onClick={() => openBatch('enable')}
        >
          {t('Batch enable')}
        </Button>
      </BulkActionsToolbar>
      {batch && (
        <UserStatusBatchDialog
          action={batch.action}
          users={batch.users}
          results={results}
          pending={mutation.isPending}
          stale={stale}
          disabled={
            props.isFetching ||
            !batch.users.every((user) => canBatchChangeUserStatus(user, actor))
          }
          onConfirm={confirm}
          onClose={() => {
            if (!submitting.current) setBatch(null)
          }}
        />
      )}
    </>
  )
}
