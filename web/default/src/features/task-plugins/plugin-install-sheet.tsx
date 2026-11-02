// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Extracted from MAX's catalog UI; QuantumNous/new-api 70e6af64 interaction reference retained.
import type { ComponentProps } from 'react'
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
}
export function PluginInstallSheet(props: Props) {
  const { t } = useTranslation()
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
        <SheetHeader>
          <SheetTitle>{t('Install plugin package')}</SheetTitle>
          <SheetDescription>
            {t(
              'Select a MAX package document. Installation retains immutable bytes and activates this version; it does not execute JavaScript.'
            )}
          </SheetDescription>
        </SheetHeader>
        <div className='space-y-4 px-4 pb-6'>
          <TaskPluginFeedback {...props.feedbackProps} showRefresh />
          <Label htmlFor='plugin-file'>{t('Package file')}</Label>
          <Input
            id='plugin-file'
            type='file'
            accept='.json,application/json'
            disabled={props.busy}
            onChange={(e) => void props.loadFile(e.target.files?.[0])}
          />
          <p className='text-muted-foreground text-xs break-all'>
            {props.fileName}
          </p>
          <Label htmlFor='plugin-pin'>{t('Trusted package SHA-256')}</Label>
          <Input
            id='plugin-pin'
            value={props.expectedHash}
            disabled={props.busy}
            onChange={(e) => {
              props.setExpectedHash(e.target.value.trim().toLowerCase())
              props.resetValidation()
            }}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Use a digest from a trusted publisher. A digest calculated from the uploaded file alone does not prove authenticity. Maximum package size: 1 MiB.'
            )}
          </p>
          <Button
            variant='outline'
            disabled={
              props.busy ||
              props.validating ||
              !props.document ||
              !/^[a-f0-9]{64}$/.test(props.expectedHash)
            }
            onClick={() => void props.validate()}
          >
            {t('Validate package')}
          </Button>
          {props.validated && (
            <div className='space-y-3 rounded-md border p-3'>
              <p>
                {props.validated.name} · {props.validated.key}@
                {props.validated.version}
              </p>
              <p>
                {t('Package format')} {props.validated.format_version} ·{' '}
                {props.validated.models.length} {t('Models')}
              </p>
              {props.validated.format_version === 2 && (
                <label className='flex items-start gap-2'>
                  <input
                    type='checkbox'
                    checked={props.nodesReady}
                    onChange={(e) => props.setNodesReady(e.target.checked)}
                  />
                  {t(
                    'All catalog nodes have been upgraded for format 2. Older nodes may suspend their entire plugin host.'
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
                  (props.validated.format_version === 2 && !props.nodesReady)
                }
                onClick={() =>
                  void props.mutate({
                    action: 'install',
                    document: props.document,
                    expected_sha256: props.expectedHash,
                    expected_revision: props.installRevision,
                    format2_nodes_ready: props.nodesReady,
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
