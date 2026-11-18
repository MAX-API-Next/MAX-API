// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Extracted from MAX's catalog UI; QuantumNous/new-api 70e6af64 interaction reference retained.
import type { ComponentProps } from 'react'
import { Package, UploadCloud, ShieldCheck, CheckCircle2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
import type { PluginEntry, PluginMutation, PluginSnapshot } from './api'
import { isJSONPluginUpload } from './file-reader'
import { TaskPluginFeedback } from './task-plugin-feedback'

type Props = {
  installOpen: boolean
  setInstallOpen: (value: boolean) => void
  busy: boolean
  disabled: boolean
  resetValidation: () => void
  feedbackProps: ComponentProps<typeof TaskPluginFeedback>
  loadFile: (file: File | undefined) => Promise<void>
  fileName: string
  expectedHash: string
  expectedHashLocked: boolean
  setExpectedHash: (value: string) => void
  document: string
  validating: boolean
  validate: () => Promise<void>
  validated: PluginEntry | undefined
  nodesReady: boolean
  setNodesReady: (value: boolean) => void
  installRevision: string
  snapshot: PluginSnapshot | undefined
  mutate: (command: PluginMutation) => Promise<void>
  bundles: { key: string; version: string; sha256: string }[]
  bundleLoading: boolean
  loadBundled: (key: string, version: string) => Promise<void>
}
export function PluginInstallSheet(props: Props) {
  const { t } = useTranslation()
  const needsNodesReady =
    !!props.validated &&
    (props.validated.format_version >= 2 || !!props.validated.bom_prefixed)
  const fileDigest =
    props.validated?.format_version === 3 && !isJSONPluginUpload(props.document)
      ? props.validated.source_sha256
      : props.validated?.digest
  return (
    <Sheet
      open={props.installOpen}
      onOpenChange={(open) => {
        if (!props.busy) {
          props.setInstallOpen(open)
          if (!open) props.resetValidation()
        }
      }}
    >
      <SheetContent className='w-full overflow-y-auto sm:max-w-xl'>
        <SheetHeader className='border-b px-6 pt-6 pr-12 pb-5'>
          <SheetTitle>{t('Install plugin package')}</SheetTitle>
          <SheetDescription>
            {t(
              'Select a bundled JavaScript plugin or a MAX package. Compatibility revisions have explicit MAX versions and source hashes. Paid task admission requires separate verification.'
            )}
          </SheetDescription>
        </SheetHeader>
        <div className='space-y-5 px-6 pb-8 text-sm'>
          <TaskPluginFeedback {...props.feedbackProps} showRefresh />
          {props.bundles.length > 0 && (
            <div className='space-y-3 rounded-xl border p-4'>
              <Label
                htmlFor='official-plugin'
                className='flex items-center gap-2'
              >
                <Package
                  className='text-muted-foreground size-4'
                  aria-hidden='true'
                />
                {t('Bundled plugins')}
              </Label>
              <select
                id='official-plugin'
                className='border-input h-9 w-full rounded-md border bg-transparent px-3 text-sm'
                disabled={props.busy || props.bundleLoading}
                value=''
                onChange={(e) => {
                  const entry = props.bundles.find(
                    (p) => `${p.key}@${p.version}` === e.target.value
                  )
                  if (entry) void props.loadBundled(entry.key, entry.version)
                }}
              >
                <option value=''>{t('Select a bundled plugin version')}</option>
                {props.bundles.map((p) => (
                  <option
                    key={`${p.key}@${p.version}`}
                    value={`${p.key}@${p.version}`}
                  >
                    {p.key} · {p.version}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className='bg-muted/20 space-y-3 rounded-xl border border-dashed p-5'>
            <Label htmlFor='plugin-file' className='flex items-center gap-2'>
              <UploadCloud
                className='text-muted-foreground size-4'
                aria-hidden='true'
              />
              {t('Package file')}
            </Label>
            <Input
              id='plugin-file'
              type='file'
              accept='.js,.json,text/javascript,application/javascript,application/json'
              disabled={props.busy}
              onChange={(e) => void props.loadFile(e.target.files?.[0])}
            />
            <p className='text-muted-foreground text-xs break-all'>
              {props.fileName || 'JavaScript / JSON · 1 MiB'}
            </p>
          </div>
          <div className='space-y-3 rounded-xl border p-4'>
            <Label htmlFor='plugin-pin' className='flex items-center gap-2'>
              <ShieldCheck
                className='text-muted-foreground size-4'
                aria-hidden='true'
              />
              {t('SHA-256 (optional)')}
            </Label>
            <Input
              id='plugin-pin'
              value={props.expectedHash}
              readOnly={props.expectedHashLocked}
              className='font-mono text-xs'
              disabled={props.busy}
              onChange={(e) => {
                props.setExpectedHash(e.target.value.trim().toLowerCase())
                props.resetValidation()
              }}
            />
            <p className='text-muted-foreground text-sm'>
              {t(
                'Leave blank to import a custom plugin. If provided, SHA-256 must match the uploaded file. The system always records content digests. Maximum package size: 1 MiB.'
              )}
            </p>
            <p className='text-muted-foreground text-xs leading-relaxed'>
              {t(
                "For official plugin.js files, use the exact version's sha256 from the publisher's index.json. Use original file bytes; changing line endings invalidates the hash."
              )}
            </p>
            <Button
              variant='outline'
              disabled={
                props.busy ||
                props.validating ||
                !props.document ||
                (props.expectedHash !== '' &&
                  !/^[a-f0-9]{64}$/.test(props.expectedHash))
              }
              onClick={() => void props.validate()}
            >
              <ShieldCheck className='size-4' aria-hidden='true' />
              {t('Validate package')}
            </Button>
          </div>
          {props.validated && (
            <div className='space-y-4 rounded-xl border border-emerald-200 bg-emerald-50/40 p-4 dark:border-emerald-900 dark:bg-emerald-950/20'>
              <p className='flex items-start gap-2 font-medium break-all'>
                <CheckCircle2
                  className='mt-0.5 size-4 shrink-0 text-emerald-600 dark:text-emerald-400'
                  aria-hidden='true'
                />
                {props.validated.name} · {props.validated.key}@
                {props.validated.version}
              </p>
              <p>
                {t('Package format')} {props.validated.format_version} ·{' '}
                {props.validated.models.length} {t('Models')}
              </p>
              <p className='font-mono text-xs break-all'>
                {t('Submitted content SHA-256')}: {fileDigest ?? '—'}
              </p>
              <p className='text-muted-foreground text-xs leading-relaxed'>
                {t(
                  'SHA-256 identifies the submitted text. Reading a file as text may remove a UTF-8 BOM; this value does not verify the original file bytes.'
                )}
              </p>
              {needsNodesReady && (
                <label className='flex items-start gap-2'>
                  <input
                    type='checkbox'
                    checked={props.nodesReady}
                    onChange={(e) => props.setNodesReady(e.target.checked)}
                  />
                  {t(
                    props.validated.bom_prefixed
                      ? 'All catalog nodes support UTF-8 BOM packages. Older nodes may suspend their entire plugin host.'
                      : 'All catalog nodes support this package format. Older nodes may suspend their entire plugin host.'
                  )}
                </label>
              )}
              {props.installRevision !== props.snapshot?.revision && (
                <p role='alert'>
                  {t(
                    'Catalog changed; reopen the confirmation before continuing.'
                  )}
                </p>
              )}
              <Button
                disabled={
                  props.disabled ||
                  props.installRevision !== props.snapshot?.revision ||
                  (needsNodesReady && !props.nodesReady)
                }
                onClick={() =>
                  void props.mutate({
                    action: 'install',
                    document: props.document,
                    expected_sha256: props.expectedHash || undefined,
                    expected_revision: props.installRevision,
                    format2_nodes_ready: props.nodesReady,
                    format3_nodes_ready: props.nodesReady,
                    bom_nodes_ready: props.validated?.bom_prefixed
                      ? props.nodesReady
                      : undefined,
                  })
                }
              >
                {t('Install and activate')}
              </Button>
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
