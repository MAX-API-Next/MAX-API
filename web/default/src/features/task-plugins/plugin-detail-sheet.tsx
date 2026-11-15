// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Extracted from MAX's catalog UI; QuantumNous/new-api 70e6af64 interaction reference retained.
import type { ComponentProps } from 'react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type {
  PluginEntry,
  PluginMutation,
  PluginSnapshot,
  PluginAction,
} from './api'
import { TaskPluginFeedback } from './task-plugin-feedback'

export type PluginConfirmation = {
  action: PluginAction
  entry: PluginEntry
  revision: string
}
type Props = {
  selected: string | undefined
  setSelected: (value: string | undefined) => void
  detail: PluginEntry | undefined
  status: (entry: PluginEntry) => string
  disabled: boolean
  busy: boolean
  loading: boolean
  installOpen: boolean
  feedbackProps: ComponentProps<typeof TaskPluginFeedback>
  confirm: PluginConfirmation | undefined
  setConfirm: (value: PluginConfirmation | undefined) => void
  nodesReady: boolean
  setNodesReady: (value: boolean) => void
  snapshot: PluginSnapshot | undefined
  mutate: (command: PluginMutation) => Promise<void>
}
export function PluginDetailSheet(props: Props) {
  const { t } = useTranslation()
  // Capture this render for delayed confirmation callbacks and type narrowing.
  const detail = props.detail
  const confirmation = props.confirm
  return (
    <Sheet
      open={!!props.selected}
      onOpenChange={(open) => {
        if (!open) {
          props.setSelected(undefined)
          props.setConfirm(undefined)
        }
      }}
    >
      <SheetContent className='w-full overflow-y-auto sm:max-w-2xl'>
        <SheetHeader>
          <SheetTitle>{t('Plugin details')}</SheetTitle>
          <SheetDescription>
            {t(
              'Declared metadata and immutable version history; execution is not available.'
            )}
          </SheetDescription>
        </SheetHeader>
        <div className='space-y-5 px-4 pb-6'>
          {!props.installOpen && (
            <TaskPluginFeedback {...props.feedbackProps} showRefresh />
          )}
          {detail ? (
            <>
              <h3 className='font-semibold'>
                {detail.name} · {detail.version}
              </h3>
              <p>
                {props.status(detail)} · {t('Package format')}{' '}
                {detail.format_version}
              </p>
              <p className='font-mono text-xs break-all'>
                {detail.format_version === 3
                  ? t('Stored package SHA-256')
                  : 'SHA-256'}
                : {detail.digest}
              </p>
              {detail.format_version === 3 && detail.source_sha256 && (
                <p className='font-mono text-xs break-all'>
                  {t('Source SHA-256')}: {detail.source_sha256}
                </p>
              )}
              <p>
                {t(
                  'Disabling blocks new bindings. Revoking permanently rejects this version and its historical sessions.'
                )}
              </p>
              <div className='flex flex-wrap gap-2'>
                {!detail.revoked && (
                  <>
                    {!detail.active && (
                      <Button
                        disabled={props.disabled}
                        onClick={() => {
                          props.setNodesReady(false)
                          props.setConfirm({
                            action: 'activate',
                            entry: detail,
                            revision: props.snapshot?.revision ?? '',
                          })
                        }}
                      >
                        {t('Activate version')}
                      </Button>
                    )}
                    {detail.active && (
                      <Button
                        disabled={props.disabled}
                        variant='outline'
                        onClick={() =>
                          props.setConfirm({
                            action: detail.enabled ? 'disable' : 'enable',
                            entry: detail,
                            revision: props.snapshot?.revision ?? '',
                          })
                        }
                      >
                        {detail.enabled ? t('Disable') : t('Enable')}
                      </Button>
                    )}
                    <Button
                      disabled={props.disabled}
                      variant='destructive'
                      onClick={() =>
                        props.setConfirm({
                          action: 'revoke',
                          entry: detail,
                          revision: props.snapshot?.revision ?? '',
                        })
                      }
                    >
                      {t('Revoke version')}
                    </Button>
                  </>
                )}
              </div>
              {confirmation && confirmation.entry.digest === detail.digest && (
                <div className='space-y-3 rounded-md border p-3'>
                  <p>
                    {t('Confirm catalog operation')}: {t(confirmation.action)} ·{' '}
                    {detail.key}@{detail.version}
                  </p>
                  {confirmation.action === 'activate' &&
                    detail.format_version >= 2 && (
                      <label className='flex items-start gap-2'>
                        <input
                          type='checkbox'
                          checked={props.nodesReady}
                          onChange={(e) =>
                            props.setNodesReady(e.target.checked)
                          }
                        />
                        {t(
                          'All catalog nodes support this package format. Older nodes may suspend their entire plugin host.'
                        )}
                      </label>
                    )}
                  {confirmation.revision !== props.snapshot?.revision && (
                    <p role='alert'>
                      {t(
                        'Catalog changed; reopen the confirmation before continuing.'
                      )}
                    </p>
                  )}
                  <Button
                    disabled={
                      props.disabled ||
                      confirmation.revision !== props.snapshot?.revision ||
                      (confirmation.action === 'activate' &&
                        detail.format_version >= 2 &&
                        !props.nodesReady)
                    }
                    onClick={() =>
                      void props.mutate({
                        action: confirmation.action,
                        key: confirmation.entry.key,
                        version: confirmation.entry.version,
                        expected_revision: confirmation.revision,
                        format2_nodes_ready: props.nodesReady,
                        format3_nodes_ready: props.nodesReady,
                      })
                    }
                  >
                    {t('Confirm')}
                  </Button>
                  <Button
                    variant='outline'
                    onClick={() => props.setConfirm(undefined)}
                  >
                    {t('Cancel')}
                  </Button>
                </div>
              )}
              <h4 className='font-medium'>{t('Declared models')}</h4>
              <ul className='space-y-1'>
                {detail.models.map((model) => (
                  <li key={model} className='break-all'>
                    {model}
                  </li>
                ))}
              </ul>
              <Link
                className='underline'
                to='/system-settings/billing/$section'
                params={{ section: 'model-pricing' }}
              >
                {t('Open model and source pricing')}
              </Link>
              <h4 className='font-medium'>
                {t('Declared protocols and routes')}
              </h4>
              <ul>
                {detail.protocols.map((p) => (
                  <li key={p.name}>
                    {p.name} {p.supports.join(', ')}
                    {p.models?.length ? (
                      <span className='text-muted-foreground block text-xs break-all'>
                        {p.models.join(', ')}
                      </span>
                    ) : null}
                  </li>
                ))}
                {detail.routes.map((r, i) => (
                  <li key={i} className='break-all'>
                    {r.method} {r.path} · {r.type}
                  </li>
                ))}
              </ul>
              <h4 className='font-medium'>{t('Model usage schemas')}</h4>
              {(detail.usage_profiles.length
                ? detail.usage_profiles
                : [
                    {
                      models: detail.models,
                      schema: detail.usage_schema,
                    },
                  ]
              ).map((profile, i) => (
                <div key={i} className='space-y-2 rounded-md border p-3'>
                  <p className='text-muted-foreground text-xs break-all'>
                    {profile.models.join(', ')}
                  </p>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Field')}</TableHead>
                        <TableHead>{t('Type')}</TableHead>
                        <TableHead>{t('Unit / values')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {Object.entries(profile.schema).map(([key, field]) => (
                        <TableRow key={key}>
                          <TableCell>{key}</TableCell>
                          <TableCell>{field.type}</TableCell>
                          <TableCell>
                            {field.unit || field.enum?.join(', ') || '—'}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              ))}
            </>
          ) : (
            props.snapshot && (
              <p>{t('The selected version is not in the current catalog')}</p>
            )
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
