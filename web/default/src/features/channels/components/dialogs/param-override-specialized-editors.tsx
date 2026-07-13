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
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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

const buildSyncTargetSpec = (type: string, key: string): string => {
  const normalizedType = type === 'header' ? 'header' : 'json'
  const normalizedKey = String(key ?? '').trim()
  if (!normalizedKey) return ''
  return `${normalizedType}:${normalizedKey}`
}

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
        <label className='text-xs font-medium'>
          {t('Error Message (required)')}
        </label>
        <Textarea
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
              <label className='text-xs font-medium'>{t('Status Code')}</label>
              <Input
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
              <label className='text-xs font-medium'>
                {t('Error Code (optional)')}
              </label>
              <Input
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
              <label className='text-xs font-medium'>
                {t('Error Type (optional)')}
              </label>
              <Input
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
        <label className='text-xs font-medium'>{t('Type (common)')}</label>
        <Input
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
              <label className='text-xs font-medium'>{t('Logic')}</label>
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
                <SelectTrigger className='h-8 text-xs'>
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
              <label className='text-xs font-medium'>
                {t('Recursion Strategy')}
              </label>
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
                {draft.rules.map((rule, ruleIndex) => (
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
                        <label className='text-[10px] font-medium'>
                          {t('Field Path')}
                        </label>
                        <Input
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
                        <label className='text-[10px] font-medium'>
                          {t('Match Mode')}
                        </label>
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
                          <SelectTrigger className='h-7 text-xs'>
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
                        <label className='text-[10px] font-medium'>
                          {t('Match Value (optional)')}
                        </label>
                        <Input
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
                      <label className='flex items-center gap-1.5 text-[10px]'>
                        <Switch
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
                      </label>
                      <label className='flex items-center gap-1.5 text-[10px]'>
                        <Switch
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
                      </label>
                    </div>
                  </div>
                ))}
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
  return (
    <div className='space-y-3'>
      <label className='text-xs font-medium'>{t('Sync Endpoints')}</label>
      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='space-y-1.5'>
          <label className='text-[10px] font-medium'>
            {t('Source Endpoint')}
          </label>
          <div className='flex gap-2'>
            <Select
              items={[
                ...SYNC_TARGET_TYPE_OPTIONS.map((o) => ({
                  value: o.value,
                  label: t(o.label),
                })),
              ]}
              value={syncFieldsEditorProps.syncFromTarget.type || 'json'}
              onValueChange={(v) =>
                v !== null &&
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  {
                    from: buildSyncTargetSpec(
                      v,
                      syncFieldsEditorProps.syncFromTarget.key
                    ),
                  }
                )
              }
            >
              <SelectTrigger className='h-8 w-[110px] text-xs'>
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
              value={syncFieldsEditorProps.syncFromTarget.key}
              onChange={(e) =>
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  {
                    from: buildSyncTargetSpec(
                      syncFieldsEditorProps.syncFromTarget.type,
                      e.target.value
                    ),
                  }
                )
              }
              placeholder='session_id'
              className='h-8 text-xs'
            />
          </div>
        </div>
        <div className='space-y-1.5'>
          <label className='text-[10px] font-medium'>
            {t('Target Endpoint')}
          </label>
          <div className='flex gap-2'>
            <Select
              items={[
                ...SYNC_TARGET_TYPE_OPTIONS.map((o) => ({
                  value: o.value,
                  label: t(o.label),
                })),
              ]}
              value={syncFieldsEditorProps.syncToTarget.type || 'json'}
              onValueChange={(v) =>
                v !== null &&
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  {
                    to: buildSyncTargetSpec(
                      v,
                      syncFieldsEditorProps.syncToTarget.key
                    ),
                  }
                )
              }
            >
              <SelectTrigger className='h-8 w-[110px] text-xs'>
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
              value={syncFieldsEditorProps.syncToTarget.key}
              onChange={(e) =>
                syncFieldsEditorProps.updateOperation(
                  syncFieldsEditorProps.operationId,
                  {
                    to: buildSyncTargetSpec(
                      syncFieldsEditorProps.syncToTarget.type,
                      e.target.value
                    ),
                  }
                )
              }
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
            onClick={() =>
              syncFieldsEditorProps.updateOperation(
                syncFieldsEditorProps.operationId,
                { from: preset.from, to: preset.to }
              )
            }
          >
            {preset.label}
          </Button>
        ))}
      </div>
    </div>
  )
}
