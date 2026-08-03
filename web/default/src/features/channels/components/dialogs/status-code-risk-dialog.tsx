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
import { Alert02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

interface StatusCodeRiskDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  detailItems: string[]
  onConfirm: () => void
}

const RISK_NOTICE_KEYS = [
  'High-risk status code retry risk check 1',
  'High-risk status code retry risk check 2',
  'High-risk status code retry risk check 3',
  'High-risk status code retry risk check 4',
] as const

interface StatusCodeRiskConfirmationContentProps {
  detailItems: string[]
  onCancel: () => void
  onConfirm: () => void
}

export function StatusCodeRiskConfirmationContent({
  detailItems,
  onCancel,
  onConfirm,
}: StatusCodeRiskConfirmationContentProps) {
  const { t } = useTranslation()

  return (
    <>
      <div className='flex flex-col gap-4'>
        {detailItems.length > 0 && (
          <div className='border-destructive/30 bg-destructive/5 rounded-lg border p-3'>
            <p className='mb-2 text-sm font-medium'>
              {t('Detected high-risk status code redirect rules')}
            </p>
            <ul className='list-inside list-disc text-sm'>
              {detailItems.map((item) => (
                <li key={item} className='font-mono text-xs'>
                  {item}
                </li>
              ))}
            </ul>
          </div>
        )}

        <ul className='text-muted-foreground flex list-disc flex-col gap-2 pl-5 text-sm'>
          {RISK_NOTICE_KEYS.map((key) => (
            <li key={key}>{t(key)}</li>
          ))}
        </ul>
      </div>

      <DialogFooter>
        <Button variant='outline' onClick={onCancel}>
          {t('Cancel')}
        </Button>
        <Button variant='destructive' onClick={onConfirm}>
          {t('I confirm enabling high-risk retry')}
        </Button>
      </DialogFooter>
    </>
  )
}

export function StatusCodeRiskDialog({
  open,
  onOpenChange,
  detailItems,
  onConfirm,
}: StatusCodeRiskDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-w-lg'>
        <DialogHeader>
          <DialogTitle className='text-destructive flex items-center gap-2'>
            <HugeiconsIcon
              icon={Alert02Icon}
              className='size-5'
              strokeWidth={2}
            />
            {t('High-risk operation confirmation')}
          </DialogTitle>
          <DialogDescription>
            {t('High-risk status code retry risk disclaimer')}
          </DialogDescription>
        </DialogHeader>

        <StatusCodeRiskConfirmationContent
          detailItems={detailItems}
          onCancel={() => onOpenChange(false)}
          onConfirm={onConfirm}
        />
      </DialogContent>
    </Dialog>
  )
}
