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
import type { ReactElement } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import {
  formatQuota,
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatLocalizedCount } from '../lib/format'
import type { BillingSettlementReconciliationItem } from '../types'

const FUNDING_SOURCE_LABEL: Record<string, string> = {
  wallet: 'Wallet',
  subscription: 'Subscription',
}

function settlementReferences(
  item: BillingSettlementReconciliationItem,
  t: TFunction
): string[] {
  const references = [t('User #{{id}}', { id: item.user_id })]
  if (item.subscription_id > 0) {
    references.push(t('Subscription #{{id}}', { id: item.subscription_id }))
  }
  if (item.token_id > 0) {
    references.push(t('Token #{{id}}', { id: item.token_id }))
  }
  if (item.task_id > 0) {
    references.push(t('Task #{{id}}', { id: item.task_id }))
  }
  return references
}

interface BillingSettlementTableProps {
  items: BillingSettlementReconciliationItem[]
  selectedIDs: ReadonlySet<number>
  reviewPending: boolean
  onSelectedIDsChange: (ids: Set<number>) => void
  onReviewItems: (items: BillingSettlementReconciliationItem[]) => void
}

export function BillingSettlementTable(
  props: BillingSettlementTableProps
): ReactElement {
  const { t, i18n } = useTranslation()
  const allSelected =
    props.items.length > 0 &&
    props.items.every((item) => props.selectedIDs.has(item.id))
  const someSelected = props.items.some((item) =>
    props.selectedIDs.has(item.id)
  )

  const toggleAll = (checked: boolean): void => {
    props.onSelectedIDsChange(
      checked ? new Set(props.items.map((item) => item.id)) : new Set()
    )
  }

  const toggleItem = (id: number, checked: boolean): void => {
    const next = new Set(props.selectedIDs)
    if (checked) next.add(id)
    else next.delete(id)
    props.onSelectedIDsChange(next)
  }

  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table className='min-w-[1450px]'>
        <TableHeader>
          <TableRow className='bg-muted/40 hover:bg-muted/40'>
            <TableHead className='w-10'>
              <Checkbox
                checked={allSelected}
                indeterminate={someSelected && !allSelected}
                onCheckedChange={(checked) => toggleAll(checked === true)}
                disabled={props.reviewPending}
                aria-label={t('Select all billing reconciliation alerts')}
              />
            </TableHead>
            <TableHead>{t('Operation')}</TableHead>
            <TableHead>{t('Financial status')}</TableHead>
            <TableHead>{t('User access')}</TableHead>
            <TableHead>{t('References')}</TableHead>
            <TableHead>{t('Funding source')}</TableHead>
            <TableHead className='text-right'>
              {t('Outstanding funding')}
            </TableHead>
            <TableHead>{t('Attempts / next retry')}</TableHead>
            <TableHead>{t('Last error')}</TableHead>
            <TableHead>{t('Created')}</TableHead>
            <TableHead className='text-right'>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.items.map((item) => {
            const outstandingFunding =
              item.funding_delta - item.applied_funding_delta
            const references = settlementReferences(item, t)
            return (
              <TableRow key={item.id}>
                <TableCell>
                  <Checkbox
                    checked={props.selectedIDs.has(item.id)}
                    onCheckedChange={(checked) =>
                      toggleItem(item.id, checked === true)
                    }
                    disabled={props.reviewPending}
                    aria-label={t(
                      'Select billing reconciliation alert {{id}}',
                      {
                        id: item.id,
                      }
                    )}
                  />
                </TableCell>
                <TableCell className='max-w-72 whitespace-normal'>
                  <span className='font-mono text-xs break-all'>
                    {item.operation_key}
                  </span>
                </TableCell>
                <TableCell>
                  <div className='flex flex-col items-start gap-1'>
                    <Badge
                      variant={
                        item.status === 'manual' ? 'destructive' : 'outline'
                      }
                    >
                      {item.status === 'manual' ? t('Manual') : t('Pending')}
                    </Badge>
                    <Badge variant='destructive'>{t('Open alert')}</Badge>
                  </div>
                </TableCell>
                <TableCell>
                  <div className='flex flex-col items-start gap-1'>
                    <Badge
                      variant={item.blocks_user ? 'destructive' : 'secondary'}
                    >
                      {item.blocks_user ? t('User blocked') : t('User allowed')}
                    </Badge>
                    <span className='text-muted-foreground text-xs'>
                      {item.record_blocks_user
                        ? t('Record policy: block')
                        : t('Record policy: allow')}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='whitespace-normal'>
                  <div className='flex max-w-48 flex-col gap-0.5 font-mono text-xs'>
                    {references.map((reference) => (
                      <span key={reference}>{reference}</span>
                    ))}
                  </div>
                </TableCell>
                <TableCell>
                  {t(FUNDING_SOURCE_LABEL[item.source] ?? item.source)}
                </TableCell>
                <TableCell className='text-right font-mono tabular-nums'>
                  {formatQuota(outstandingFunding)}
                </TableCell>
                <TableCell className='whitespace-normal'>
                  <div className='flex max-w-44 flex-col gap-0.5 text-xs'>
                    <span>
                      {formatLocalizedCount(
                        item.attempts,
                        i18n.language,
                        t,
                        '{{count}} attempt',
                        '{{count}} attempts'
                      )}
                    </span>
                    <span
                      className='text-muted-foreground'
                      title={formatTimestampToDate(item.next_attempt)}
                    >
                      {item.status === 'manual'
                        ? t('Manual review')
                        : formatTimestampRelative(
                            item.next_attempt,
                            'seconds',
                            i18n.language
                          )}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='max-w-72 whitespace-normal'>
                  <span
                    className='text-muted-foreground text-xs break-words'
                    title={item.last_error || undefined}
                  >
                    {item.last_error || t('No error detail')}
                  </span>
                </TableCell>
                <TableCell
                  className='text-muted-foreground text-xs'
                  title={formatTimestampToDate(item.created_at)}
                >
                  {formatTimestampRelative(
                    item.created_at,
                    'seconds',
                    i18n.language
                  )}
                </TableCell>
                <TableCell className='text-right'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => props.onReviewItems([item])}
                    disabled={props.reviewPending}
                  >
                    {t('Review and close')}
                  </Button>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
