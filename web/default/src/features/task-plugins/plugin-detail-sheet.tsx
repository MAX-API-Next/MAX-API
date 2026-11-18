// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Extracted from MAX's catalog UI; QuantumNous/new-api 70e6af64 interaction reference retained.
import type { ComponentProps, RefObject } from 'react'
import { Link } from '@tanstack/react-router'
import {
  Package,
  ArrowUpRight,
  ShieldCheck,
  ChevronDown,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
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
import { packageCompatibilityMessage } from './package-compatibility'
import { PluginStatusBadge } from './plugin-catalog'
import { TaskPluginFeedback } from './task-plugin-feedback'

export type PluginConfirmation = {
  action: PluginAction
  entry: PluginEntry
  revision: string
}
type Props = {
  finalFocus: RefObject<HTMLInputElement | null>
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
      <SheetContent
        finalFocus={props.finalFocus}
        className='w-full overflow-y-auto sm:max-w-2xl'
      >
        <SheetHeader className='border-b px-6 pt-6 pr-12 pb-5'>
          <SheetTitle>{t('Plugin details')}</SheetTitle>
          <SheetDescription>
            {t(
              'Declared metadata and immutable version history; execution is not available.'
            )}
          </SheetDescription>
        </SheetHeader>
        <div className='space-y-5 px-6 pb-8 text-sm'>
          {!props.installOpen && (
            <TaskPluginFeedback {...props.feedbackProps} showRefresh />
          )}
          {detail ? (
            <>
              <div className='flex min-w-0 items-center gap-3 pt-1'>
                <div className='bg-muted/60 flex size-12 shrink-0 items-center justify-center rounded-xl border'>
                  <Package
                    className='text-muted-foreground size-6'
                    aria-hidden='true'
                  />
                </div>
                <div className='min-w-0 flex-1'>
                  <h3 className='text-base font-semibold break-words'>
                    {detail.name}
                  </h3>
                  <p className='text-muted-foreground mt-1 font-mono text-xs break-all'>
                    {detail.key} · {detail.version}
                  </p>
                </div>
              </div>
              <div className='flex flex-wrap gap-2'>
                <PluginStatusBadge entry={detail}>
                  {props.status(detail)}
                </PluginStatusBadge>
                <Badge variant='secondary' className='font-normal'>
                  {t('Package format')} {detail.format_version}
                </Badge>
              </div>
              <details className='group bg-muted/20 rounded-lg border'>
                <summary className='flex cursor-pointer items-center gap-2 px-4 py-3 text-xs font-medium'>
                  <ShieldCheck className='size-4' aria-hidden='true' />
                  {t('Package integrity')}
                  <ChevronDown
                    className='ml-auto size-4 transition-transform group-open:rotate-180'
                    aria-hidden='true'
                  />
                </summary>
                <div className='space-y-3 border-t px-4 py-3'>
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
                </div>
              </details>
              <p className='text-muted-foreground bg-muted/40 rounded-lg p-3 text-xs leading-relaxed'>
                {t(
                  'Disabling blocks new bindings while keeping installed files and historical tasks available.'
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
                  </>
                )}
                <Button
                  disabled={props.disabled}
                  variant='destructive'
                  onClick={() => {
                    props.setNodesReady(false)
                    props.setConfirm({
                      action: 'delete',
                      entry: detail,
                      revision: props.snapshot?.revision ?? '',
                    })
                  }}
                >
                  <Trash2 className='size-4' aria-hidden='true' />
                  {t('Delete version')}
                </Button>
              </div>
              <p className='text-muted-foreground text-xs leading-relaxed'>
                {t(
                  detail.revoked
                    ? 'Delete directly when there are no task, channel or pricing references. Installed source is removed; distribution bundles, audit identity and historical revocation are retained. Re-importing the same file remains blocked.'
                    : 'Delete directly when there are no task, channel or pricing references. Installed source is removed; distribution bundles and audit identity are retained. The same file can be imported again.'
                )}
              </p>
              {confirmation && confirmation.entry.digest === detail.digest && (
                <div className='space-y-3 rounded-lg border border-amber-200 bg-amber-50/50 p-4 dark:border-amber-900 dark:bg-amber-950/20'>
                  <p>
                    {t('Confirm catalog operation')}:{' '}
                    {confirmation.action === 'delete'
                      ? t('Delete version')
                      : t(confirmation.action)}{' '}
                    · {detail.key}@{detail.version}
                  </p>
                  {confirmation.action === 'delete' && (
                    <label className='flex items-start gap-2'>
                      <input
                        type='checkbox'
                        checked={props.nodesReady}
                        onChange={(e) => props.setNodesReady(e.target.checked)}
                      />
                      {t(
                        'All catalog nodes have been upgraded to support version deletion. Older nodes may suspend their plugin host after deletion.'
                      )}
                    </label>
                  )}
                  {confirmation.action === 'activate' &&
                    (detail.format_version >= 2 || detail.bom_prefixed) && (
                      <label className='flex items-start gap-2'>
                        <input
                          type='checkbox'
                          checked={props.nodesReady}
                          onChange={(e) =>
                            props.setNodesReady(e.target.checked)
                          }
                        />
                        {t(packageCompatibilityMessage(detail))}
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
                      (confirmation.action === 'delete' && !props.nodesReady) ||
                      (confirmation.action === 'activate' &&
                        (detail.format_version >= 2 || detail.bom_prefixed) &&
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
                        bom_nodes_ready: detail.bom_prefixed
                          ? props.nodesReady
                          : undefined,
                        deletion_nodes_ready: props.nodesReady,
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
              <h4 className='border-b pb-2 font-semibold'>
                {t('Declared models')}
              </h4>
              <ul className='flex flex-wrap gap-2'>
                {detail.models.map((model) => (
                  <li
                    key={model}
                    className='bg-muted/60 max-w-full rounded-md border px-2 py-1 font-mono text-xs break-all'
                  >
                    {model}
                  </li>
                ))}
              </ul>
              <Link
                className='text-primary inline-flex items-center gap-1.5 text-xs font-medium hover:underline'
                to='/system-settings/billing/$section'
                params={{ section: 'model-pricing' }}
              >
                {t('Open model and source pricing')}
                <ArrowUpRight className='size-3.5' aria-hidden='true' />
              </Link>
              <h4 className='border-b pb-2 font-semibold'>
                {t('Declared protocols and routes')}
              </h4>
              <ul className='divide-y rounded-lg border px-4'>
                {detail.protocols.map((p) => (
                  <li key={p.name} className='py-3 text-xs break-all'>
                    {p.name} {p.supports.join(', ')}
                    {p.models?.length ? (
                      <span className='text-muted-foreground block text-xs break-all'>
                        {p.models.join(', ')}
                      </span>
                    ) : null}
                  </li>
                ))}
                {detail.routes.map((r) => (
                  <li
                    key={`${r.method} ${r.path}`}
                    className='py-3 font-mono text-xs break-all'
                  >
                    {r.method} {r.path} · {r.type}
                  </li>
                ))}
              </ul>
              <h4 className='border-b pb-2 font-semibold'>
                {t('Model usage schemas')}
              </h4>
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
