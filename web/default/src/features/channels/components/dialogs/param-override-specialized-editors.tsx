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
import { useId, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  editSyncTargetKey,
  normalizeSyncTargetType,
  selectSyncTargetType,
  type SyncTargetType,
} from './param-override-specialized-editor-utils'

const CONDITION_MODE_OPTIONS = [
  { label: 'Exact Match', value: 'full' },
  { label: 'Prefix', value: 'prefix' },
  { label: 'Suffix', value: 'suffix' },
  { label: 'Contains', value: 'contains' },
  { label: 'Greater Than', value: 'gt' },
  { label: 'Greater Than or Equal', value: 'gte' },
  { label: 'Less Than', value: 'lt' },
  { label: 'Less Than or Equal', value: 'lte' },
]

const SYNC_TARGET_TYPE_OPTIONS = [
  { label: 'Request Body Field', value: 'json' },
  { label: 'Request Header Field', value: 'header' },
]

export type ReturnErrorDraft = {
  message: string
  statusCode: number
  code: string
  type: string
  skipRetry: boolean
  simpleMode: boolean
}

export type PruneRule = {
  id: string
  path: string
  mode: string
  value_text: string
  invert: boolean
  pass_missing_key: boolean
}

export type PruneObjectsDraft = {
  simpleMode: boolean
  typeText: string
  logic: string
  recursive: boolean
  rules: PruneRule[]
}

// ---------------------------------------------------------------------------
// ReturnErrorEditor
// ---------------------------------------------------------------------------

type ReturnErrorEditorProps = {
  operationId: string
  draft: ReturnErrorDraft
  updateDraft: (
    operationId: string,
    draftPatch: Partial<ReturnErrorDraft>
  ) => void
}

export function ReturnErrorEditor(
  returnErrorEditorProps: ReturnErrorEditorProps
) {
  const { t } = useTranslation()
  const draft = returnErrorEditorProps.draft
  const fieldIdPrefix = useId()

  return (
    <div className='rounded-lg border p-3'>
      <div className='mb-2 flex items-center justify-between'>
        <span className='text-sm font-medium'>
          {t('Custom Error Response')}
        </span>
        <div className='flex items-center gap-1'>
          <span className='text-muted-foreground text-xs'>{t('Mode')}</span>
          <Button
            type='button'
            variant={draft.simpleMode ? 'default' : 'outline'}
            size='sm'
            className='h-7 text-xs'
            onClick={() =>
              returnErrorEditorProps.updateDraft(
                returnErrorEditorProps.operationId,
                { simpleMode: true }
              )
            }
          >
            {t('Simple')}
          </Button>
          <Button
            type='button'
            variant={draft.simpleMode ? 'outline' : 'default'}
            size='sm'
            className='h-7 text-xs'
            onClick={() =>
              returnErrorEditorProps.updateDraft(
                returnErrorEditorProps.operationId,
                { simpleMode: false }
              )
            }
          >
            {t('Advanced')}
          </Button>
        </div>
      </div>

      <div className='space-y-1.5'>
        <Label
          htmlFor={`${fieldIdPrefix}-message`}
          className='text-xs font-medium'
        >
          {t('Error Message (required)')}
        </Label>
        <Textarea
          id={`${fieldIdPrefix}-message`}
          value={draft.message}
          onChange={(e) =>
            returnErrorEditorProps.updateDraft(
              returnErrorEditorProps.operationId,
              { message: e.target.value }
            )
          }
          placeholder={t('e.g. This request does not meet access policy')}
          rows={2}
          className='text-xs'
        />
      </div>

      {draft.simpleMode ? (
        <p className='text-muted-foreground mt-2 text-xs'>
          {t(
            'Simple mode only returns message; status code and error type use system defaults.'
          )}
        </p>
      ) : (
        <>
          <div className='mt-3 grid gap-3 sm:grid-cols-3'>
            <div className='space-y-1'>
              <Label
                htmlFor={`${fieldIdPrefix}-status-code`}
                className='text-xs font-medium'
              >
                {t('Status Code')}
              </Label>
              <Input
                id={`${fieldIdPrefix}-status-code`}
                value={String(draft.statusCode ?? '')}
                onChange={(e) =>
                  returnErrorEditorProps.updateDraft(
                    returnErrorEditorProps.operationId,
                    { statusCode: parseInt(e.target.value, 10) || 400 }
                  )
                }
                placeholder='400'
                className='h-8 text-xs'
              />
            </div>
            <div className='space-y-1'>
              <Label
                htmlFor={`${fieldIdPrefix}-error-code`}
                className='text-xs font-medium'
              >
                {t('Error Code (optional)')}
              </Label>
              <Input
                id={`${fieldIdPrefix}-error-code`}
                value={draft.code}
                onChange={(e) =>
                  returnErrorEditorProps.updateDraft(
                    returnErrorEditorProps.operationId,
                    { code: e.target.value }
                  )
                }
                placeholder='forced_bad_request'
                className='h-8 text-xs'
              />
            </div>
            <div className='space-y-1'>
              <Label
                htmlFor={`${fieldIdPrefix}-error-type`}
                className='text-xs font-medium'
              >
                {t('Error Type (optional)')}
              </Label>
              <Input
                id={`${fieldIdPrefix}-error-type`}
                value={draft.type}
                onChange={(e) =>
                  returnErrorEditorProps.updateDraft(
                    returnErrorEditorProps.operationId,
                    { type: e.target.value }
                  )
                }
                placeholder='invalid_request_error'
                className='h-8 text-xs'
              />
            </div>
          </div>
          <div className='mt-2 flex items-center gap-2'>
            <span className='text-muted-foreground text-xs'>
              {t('Retry Suggestion')}
            </span>
            <Button
              type='button'
              variant={draft.skipRetry ? 'default' : 'outline'}
              size='sm'
              className='h-7 text-xs'
              onClick={() =>
                returnErrorEditorProps.updateDraft(
                  returnErrorEditorProps.operationId,
                  { skipRetry: true }
                )
              }
            >
              {t('Stop Retry')}
            </Button>
            <Button
              type='button'
              variant={draft.skipRetry ? 'outline' : 'default'}
              size='sm'
              className='h-7 text-xs'
              onClick={() =>
                returnErrorEditorProps.updateDraft(
                  returnErrorEditorProps.operationId,
                  { skipRetry: false }
                )
              }
            >
              {t('Allow Retry')}
            </Button>
          </div>
          <div className='mt-2 flex flex-wrap gap-1'>
            {[
              {
                label: 'Bad Request',
                statusCode: 400,
                code: 'invalid_request',
                type: 'invalid_request_error',
              },
              {
                label: 'Unauthorized',
                statusCode: 401,
                code: 'unauthorized',
                type: 'authentication_error',
              },
              {
                label: 'Rate Limited',
                statusCode: 429,
                code: 'rate_limited',
                type: 'rate_limit_error',
              },
            ].map((preset) => (
              <Button
                key={preset.code}
                type='button'
                variant='outline'
                size='sm'
                className='h-6 text-[10px]'
                onClick={() =>
                  returnErrorEditorProps.updateDraft(
                    returnErrorEditorProps.operationId,
                    {
                      statusCode: preset.statusCode,
                      code: preset.code,
                      type: preset.type,
                    }
                  )
                }
              >
                {t(preset.label)}
              </Button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// PruneObjectsEditor
// ---------------------------------------------------------------------------

type PruneObjectsEditorProps = {
  operationId: string
  draft: PruneObjectsDraft
  updateDraft: (
    operationId: string,
    updater:
      | Partial<PruneObjectsDraft>
      | ((draft: PruneObjectsDraft) => PruneObjectsDraft)
  ) => void
  addRule: (operationId: string) => void
  updateRule: (
    operationId: string,
    ruleId: string,
    patch: Partial<PruneRule>
  ) => void
  removeRule: (operationId: string, ruleId: string) => void
}

export function PruneObjectsEditor(
  pruneObjectsEditorProps: PruneObjectsEditorProps
) {
  const { t } = useTranslation()
  const draft = pruneObjectsEditorProps.draft
  const fieldIdPrefix = useId()

  return (
    <div className='rounded-lg border p-3'>
      <div className='mb-2 flex items-center justify-between'>
        <span className='text-sm font-medium'>{t('Object Prune Rules')}</span>
        <div className='flex items-center gap-1'>
          <span className='text-muted-foreground text-xs'>{t('Mode')}</span>
          <Button
            type='button'
            variant={draft.simpleMode ? 'default' : 'outline'}
            size='sm'
            className='h-7 text-xs'
            onClick={() =>
              pruneObjectsEditorProps.updateDraft(
                pruneObjectsEditorProps.operationId,
                { simpleMode: true }
              )
            }
          >
            {t('Simple')}
          </Button>
          <Button
            type='button'
            variant={draft.simpleMode ? 'outline' : 'default'}
            size='sm'
            className='h-7 text-xs'
            onClick={() =>
              pruneObjectsEditorProps.updateDraft(
                pruneObjectsEditorProps.operationId,
                { simpleMode: false }
              )
            }
          >
            {t('Advanced')}
          </Button>
        </div>
      </div>

      <div className='space-y-1.5'>
        <Label
          htmlFor={`${fieldIdPrefix}-type`}
          className='text-xs font-medium'
        >
          {t('Type (common)')}
        </Label>
        <Input
          id={`${fieldIdPrefix}-type`}
          value={draft.typeText}
          onChange={(e) =>
            pruneObjectsEditorProps.updateDraft(
              pruneObjectsEditorProps.operationId,
              { typeText: e.target.value }
            )
          }
          placeholder='redacted_thinking'
          className='h-8 text-xs'
        />
      </div>

      {draft.simpleMode ? (
        <p className='text-muted-foreground mt-2 text-xs'>
          {t('Simple mode: prune objects by type, e.g. redacted_thinking.')}
        </p>
      ) : (
        <>
          <div className='mt-3 grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1'>
              <Label
                htmlFor={`${fieldIdPrefix}-logic`}
                className='text-xs font-medium'
              >
                {t('Logic')}
              </Label>
              <Select
                items={[
                  { value: 'AND', label: t('All Must Match (AND)') },
                  { value: 'OR', label: t('Any Match (OR)') },
                ]}
                value={draft.logic}
                onValueChange={(v) =>
                  pruneObjectsEditorProps.updateDraft(
                    pruneObjectsEditorProps.operationId,
                    { logic: v || 'AND' }
                  )
                }
              >
                <SelectTrigger
                  id={`${fieldIdPrefix}-logic`}
                  className='h-8 text-xs'
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectItem value='AND'>
                      {t('All Must Match (AND)')}
                    </SelectItem>
                    <SelectItem value='OR'>{t('Any Match (OR)')}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1'>
              <span className='text-xs font-medium'>
                {t('Recursion Strategy')}
              </span>
              <div className='flex gap-1'>
                <Button
                  type='button'
                  variant={draft.recursive ? 'default' : 'outline'}
                  size='sm'
                  className='h-8 text-xs'
                  onClick={() =>
                    pruneObjectsEditorProps.updateDraft(
                      pruneObjectsEditorProps.operationId,
                      { recursive: true }
                    )
                  }
                >
                  {t('Recursive')}
                </Button>
                <Button
                  type='button'
                  variant={draft.recursive ? 'outline' : 'default'}
                  size='sm'
                  className='h-8 text-xs'
                  onClick={() =>
                    pruneObjectsEditorProps.updateDraft(
                      pruneObjectsEditorProps.operationId,
                      { recursive: false }
                    )
                  }
                >
                  {t('Current Level Only')}
                </Button>
              </div>
            </div>
          </div>

          <div className='bg-muted/30 mt-3 rounded-md border p-2'>
            <div className='mb-2 flex items-center justify-between'>
              <span className='text-xs font-medium'>
                {t('Additional Conditions')}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='h-7 text-xs'
                onClick={() =>
                  pruneObjectsEditorProps.addRule(
                    pruneObjectsEditorProps.operationId
                  )
                }
              >
                <Plus className='mr-1 h-3 w-3' />
                {t('Add Condition')}
              </Button>
            </div>
            {draft.rules.length === 0 ? (
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Without additional conditions, only the type above is used for pruning.'
                )}
              </p>
            ) : (
              <div className='space-y-2'>
                {draft.rules.map((rule, ruleIndex) => {
                  const ruleFieldIdPrefix = `${fieldIdPrefix}-rule-${rule.id}`

                  return (
                    <div
                      key={rule.id}
                      className='bg-background rounded-md border p-2'
                    >
                      <div className='mb-1 flex items-center justify-between'>
                        <Badge variant='outline' className='text-[10px]'>
                          R{ruleIndex + 1}
                        </Badge>
                        <Button
                          type='button'
                          variant='ghost'
                          size='sm'
                          className='text-destructive hover:text-destructive h-6 text-[10px]'
                          onClick={() =>
                            pruneObjectsEditorProps.removeRule(
                              pruneObjectsEditorProps.operationId,
                              rule.id
                            )
                          }
                        >
                          <Trash2 className='mr-1 h-3 w-3' />
                          {t('Delete')}
                        </Button>
                      </div>
                      <div className='grid gap-2 sm:grid-cols-3'>
                        <div className='space-y-0.5'>
                          <Label
                            htmlFor={`${ruleFieldIdPrefix}-path`}
                            className='text-[10px] font-medium'
                          >
                            {t('Field Path')}
                          </Label>
                          <Input
                            id={`${ruleFieldIdPrefix}-path`}
                            value={rule.path}
                            onChange={(e) =>
                              pruneObjectsEditorProps.updateRule(
                                pruneObjectsEditorProps.operationId,
                                rule.id,
                                { path: e.target.value }
                              )
                            }
                            placeholder='type'
                            className='h-7 text-xs'
                          />
                        </div>
                        <div className='space-y-0.5'>
                          <Label
                            htmlFor={`${ruleFieldIdPrefix}-mode`}
                            className='text-[10px] font-medium'
                          >
                            {t('Match Mode')}
                          </Label>
                          <Select
                            items={[
                              ...CONDITION_MODE_OPTIONS.map((o) => ({
                                value: o.value,
                                label: t(o.label),
                              })),
                            ]}
                            value={rule.mode}
                            onValueChange={(v) =>
                              v !== null &&
                              pruneObjectsEditorProps.updateRule(
                                pruneObjectsEditorProps.operationId,
                                rule.id,
                                { mode: v }
                              )
                            }
                          >
                            <SelectTrigger
                              id={`${ruleFieldIdPrefix}-mode`}
                              className='h-7 text-xs'
                            >
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent alignItemWithTrigger={false}>
                              <SelectGroup>
                                {CONDITION_MODE_OPTIONS.map((o) => (
                                  <SelectItem key={o.value} value={o.value}>
                                    {t(o.label)}
                                  </SelectItem>
                                ))}
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                        </div>
                        <div className='space-y-0.5'>
                          <Label
                            htmlFor={`${ruleFieldIdPrefix}-value`}
                            className='text-[10px] font-medium'
                          >
                            {t('Match Value (optional)')}
                          </Label>
                          <Input
                            id={`${ruleFieldIdPrefix}-value`}
                            value={rule.value_text}
                            onChange={(e) =>
                              pruneObjectsEditorProps.updateRule(
                                pruneObjectsEditorProps.operationId,
                                rule.id,
                                { value_text: e.target.value }
                              )
                            }
                            placeholder='redacted_thinking'
                            className='h-7 text-xs'
                          />
                        </div>
                      </div>
                      <div className='mt-1.5 flex flex-wrap gap-3'>
                        <Label
                          htmlFor={`${ruleFieldIdPrefix}-invert`}
                          className='flex items-center gap-1.5 text-[10px]'
                        >
                          <Switch
                            id={`${ruleFieldIdPrefix}-invert`}
                            checked={rule.invert}
                            onCheckedChange={(checked) =>
                              pruneObjectsEditorProps.updateRule(
                                pruneObjectsEditorProps.operationId,
                                rule.id,
                                { invert: checked }
                              )
                            }
                          />
                          {t('Invert match')}
                        </Label>
                        <Label
                          htmlFor={`${ruleFieldIdPrefix}-pass-missing`}
                          className='flex items-center gap-1.5 text-[10px]'
                        >
                          <Switch
                            id={`${ruleFieldIdPrefix}-pass-missing`}
                            checked={rule.pass_missing_key}
                            onCheckedChange={(checked) =>
                              pruneObjectsEditorProps.updateRule(
                                pruneObjectsEditorProps.operationId,
                                rule.id,
                                { pass_missing_key: checked }
                              )
                            }
                          />
                          {t('Pass when key is missing')}
                        </Label>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// SyncFieldsEditor
// ---------------------------------------------------------------------------

type SyncFieldsEditorProps = {
  operationId: string
  syncFromTarget: { type: string; key: string }
  syncToTarget: { type: string; key: string }
  updateOperation: (
    operationId: string,
    patch: Partial<{ from: string; to: string }>
  ) => void
}

export function SyncFieldsEditor(syncFieldsEditorProps: SyncFieldsEditorProps) {
  const { t } = useTranslation()
  const fieldIdPrefix = useId()
  const [sourceTypeOverride, setSourceTypeOverride] =
    useState<SyncTargetType | null>(null)
  const [targetTypeOverride, setTargetTypeOverride] =
    useState<SyncTargetType | null>(null)
  const sourceType = syncFieldsEditorProps.syncFromTarget.key
    ? normalizeSyncTargetType(syncFieldsEditorProps.syncFromTarget.type)
    : (sourceTypeOverride ??
      normalizeSyncTargetType(syncFieldsEditorProps.syncFromTarget.type))
  const targetType = syncFieldsEditorProps.syncToTarget.key
    ? normalizeSyncTargetType(syncFieldsEditorProps.syncToTarget.type)
    : (targetTypeOverride ??
      normalizeSyncTargetType(syncFieldsEditorProps.syncToTarget.type))

  return (
    <div className='space-y-3'>
      <p className='text-xs font-medium'>{t('Sync Endpoints')}</p>
      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='space-y-1.5'>
          <Label
            id={`${fieldIdPrefix}-source-label`}
            htmlFor={`${fieldIdPrefix}-source-key`}
            className='text-[10px] font-medium'
          >
            {t('Source Endpoint')}
          </Label>
          <div className='flex gap-2'>
            <Select
              items={[
                ...SYNC_TARGET_TYPE_OPTIONS.map((o) => ({
                  value: o.value,
                  label: t(o.label),
                })),
              ]}
              value={sourceType}
              onValueChange={(value) => {
                if (value === null) return
                const edit = selectSyncTargetType(
                  syncFieldsEditorProps.syncFromTarget,
                  value
                )
                setSourceTypeOverride(edit.typeOverride)
                if (!edit.spec) return
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  { from: edit.spec }
                )
              }}
            >
              <SelectTrigger
                id={`${fieldIdPrefix}-source-type`}
                aria-labelledby={`${fieldIdPrefix}-source-label`}
                className='h-8 w-[110px] text-xs'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {SYNC_TARGET_TYPE_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {t(o.label)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Input
              id={`${fieldIdPrefix}-source-key`}
              value={syncFieldsEditorProps.syncFromTarget.key}
              onChange={(e) => {
                const edit = editSyncTargetKey(sourceType, e.target.value)
                setSourceTypeOverride(edit.typeOverride)
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  { from: edit.spec }
                )
              }}
              placeholder='session_id'
              className='h-8 text-xs'
            />
          </div>
        </div>
        <div className='space-y-1.5'>
          <Label
            id={`${fieldIdPrefix}-target-label`}
            htmlFor={`${fieldIdPrefix}-target-key`}
            className='text-[10px] font-medium'
          >
            {t('Target Endpoint')}
          </Label>
          <div className='flex gap-2'>
            <Select
              items={[
                ...SYNC_TARGET_TYPE_OPTIONS.map((o) => ({
                  value: o.value,
                  label: t(o.label),
                })),
              ]}
              value={targetType}
              onValueChange={(value) => {
                if (value === null) return
                const edit = selectSyncTargetType(
                  syncFieldsEditorProps.syncToTarget,
                  value
                )
                setTargetTypeOverride(edit.typeOverride)
                if (!edit.spec) return
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  { to: edit.spec }
                )
              }}
            >
              <SelectTrigger
                id={`${fieldIdPrefix}-target-type`}
                aria-labelledby={`${fieldIdPrefix}-target-label`}
                className='h-8 w-[110px] text-xs'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {SYNC_TARGET_TYPE_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {t(o.label)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Input
              id={`${fieldIdPrefix}-target-key`}
              value={syncFieldsEditorProps.syncToTarget.key}
              onChange={(e) => {
                const edit = editSyncTargetKey(targetType, e.target.value)
                setTargetTypeOverride(edit.typeOverride)
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  { to: edit.spec }
                )
              }}
              placeholder='prompt_cache_key'
              className='h-8 text-xs'
            />
          </div>
        </div>
      </div>
      <div className='flex flex-wrap gap-1'>
        {[
          {
            label: 'header:session_id -> json:prompt_cache_key',
            from: 'header:session_id',
            to: 'json:prompt_cache_key',
          },
          {
            label: 'json:prompt_cache_key -> header:session_id',
            from: 'json:prompt_cache_key',
            to: 'header:session_id',
          },
        ].map((preset) => (
          <Button
            key={preset.label}
            type='button'
            variant='outline'
            size='sm'
            className='h-6 text-[10px]'
            onClick={() => {
              setSourceTypeOverride(null)
              setTargetTypeOverride(null)
              syncFieldsEditorProps.updateOperation(
                syncFieldsEditorProps.operationId,
                { from: preset.from, to: preset.to }
              )
            }}
          >
            {preset.label}
          </Button>
        ))}
      </div>
    </div>
  )
}
