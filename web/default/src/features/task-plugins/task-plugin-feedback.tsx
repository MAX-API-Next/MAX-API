// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

type TaskPluginFeedbackProps = {
  error: string
  readError: string
  notice: string
  unknown: boolean
  readAfterUnknown: boolean
  busy: boolean
  loading: boolean
  showRefresh?: boolean
  onRefresh: () => void
  onContinue: () => void
}

// Render feedback in the active surface so modal isolation cannot hide it.
export function TaskPluginFeedback(props: TaskPluginFeedbackProps) {
  const { t } = useTranslation()
  return (
    <>
      {props.error && (
        <p role='alert' className='text-destructive'>
          {t(props.error)}
        </p>
      )}
      {props.readError && (
        <p role='alert' className='text-destructive'>
          {t(props.readError)}
        </p>
      )}
      {props.notice && <p role='status'>{t(props.notice)}</p>}
      {props.showRefresh && (props.readError || props.unknown) && (
        <Button
          variant='outline'
          disabled={props.busy || props.loading}
          onClick={props.onRefresh}
        >
          {t('Refresh')}
        </Button>
      )}
      {props.unknown && (
        <div role='alert' className='space-y-2 rounded-md border p-3'>
          <p>
            {t(
              'The mutation result is unknown. The write was not retried. Rereading the catalog cannot prove the earlier request failed or finished.'
            )}
          </p>
          <Button
            variant='outline'
            disabled={!props.readAfterUnknown || props.busy || props.loading}
            onClick={props.onContinue}
          >
            {t('I have checked the catalog; continue')}
          </Button>
        </div>
      )}
    </>
  )
}
