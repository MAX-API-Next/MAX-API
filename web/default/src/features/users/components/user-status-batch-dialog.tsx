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
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import type {
  BatchUserStatusAction,
  BatchUserStatusResult,
  User,
} from '../types'

interface UserStatusBatchDialogProps {
  action: BatchUserStatusAction
  users: User[]
  results: BatchUserStatusResult[] | null
  pending: boolean
  stale: boolean
  disabled: boolean
  onConfirm: () => void
  onClose: () => void
}

export function UserStatusBatchDialog(props: UserStatusBatchDialogProps) {
  const { t } = useTranslation()
  const title =
    props.action === 'disable' ? t('Batch disable') : t('Batch enable')
  const label = (result: BatchUserStatusResult) => {
    if (result.outcome === 'updated') return t('Updated')
    if (result.outcome === 'unchanged') return t('No change needed')
    if (result.outcome === 'unknown')
      return t('Result unconfirmed. Refresh and verify before retrying.')
    if (result.code === 'forbidden')
      return t('Protected account or insufficient permissions')
    if (result.code === 'not_found') return t('User not found or deleted')
    if (result.code === 'conflict')
      return t('Account status changed. Refresh and select again.')
    return t('Operation rejected')
  }
  const confirmed =
    props.results?.filter(
      (result) => result.outcome === 'updated' || result.outcome === 'unchanged'
    ).length ?? 0
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !props.pending) props.onClose()
      }}
    >
      <DialogContent
        className='max-h-[85dvh] overflow-y-auto sm:max-w-lg'
        showCloseButton={!props.pending}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {t('Review the {{count}} selected accounts before confirming.', {
              count: props.users.length,
            })}
          </DialogDescription>
        </DialogHeader>
        <Alert>
          <AlertDescription>
            {props.action === 'disable'
              ? t(
                  'Disabling blocks new sign-ins and API requests. Existing upstream tasks are not cancelled. Balances and records are kept.'
                )
              : t(
                  'Enabling restores account access, including existing valid API keys. Balances and records are unchanged.'
                )}
          </AlertDescription>
        </Alert>
        {props.stale && !props.results && (
          <Alert variant='destructive'>
            <AlertDescription>
              {t(
                'The user list changed. Close this dialog and select accounts again.'
              )}
            </AlertDescription>
          </Alert>
        )}
        {props.results && (
          <p role='status'>
            {t(
              '{{confirmed}} confirmed, {{remaining}} rejected or unconfirmed.',
              { confirmed, remaining: props.results.length - confirmed }
            )}
          </p>
        )}
        <ul
          className='max-h-[35dvh] divide-y overflow-y-auto'
          aria-label={t('Selected users')}
        >
          {props.users.map((user) => {
            const result = props.results?.find((item) => item.id === user.id)
            return (
              <li key={user.id} className='flex flex-col gap-1 py-2'>
                <div className='flex min-w-0 items-start gap-2'>
                  <span className='text-muted-foreground shrink-0 tabular-nums'>
                    #{user.id}
                  </span>
                  <span className='min-w-0 break-all'>{user.username}</span>
                  {(!result || result.status === 1 || result.status === 2) && (
                    <Badge variant='outline' className='ml-auto shrink-0'>
                      {(result?.status ?? user.status) === 1
                        ? t('Enabled')
                        : t('Disabled')}
                    </Badge>
                  )}
                </div>
                {result && (
                  <p
                    className='text-muted-foreground text-sm'
                    data-outcome={result.outcome}
                  >
                    {label(result)}
                  </p>
                )}
              </li>
            )
          })}
        </ul>
        <DialogFooter>
          <Button
            variant='outline'
            disabled={props.pending}
            onClick={props.onClose}
          >
            {props.results ? t('Close') : t('Cancel')}
          </Button>
          {!props.results && (
            <Button
              variant={props.action === 'disable' ? 'destructive' : 'default'}
              disabled={props.pending || props.stale || props.disabled}
              onClick={props.onConfirm}
            >
              {props.pending && <Spinner data-icon='inline-start' />}
              {t('Confirm')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
