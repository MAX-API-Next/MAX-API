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
import { useCallback, useMemo } from 'react'
import { useFormContext, useWatch } from 'react-hook-form'
import {
  ArrowRight,
  Boxes,
  Copy,
  Eraser,
  FileText,
  HelpCircle,
  Plus,
  Sparkles,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { MultiSelect, type Option } from '@/components/multi-select'
import {
  FIELD_DESCRIPTIONS,
  FIELD_PLACEHOLDERS,
  MODEL_FETCHABLE_TYPES,
} from '../../../constants'
import {
  formatModelsArray,
  parseModelsString,
  type ChannelFormValues,
} from '../../../lib'
import { ModelMappingEditor } from '../../model-mapping-editor'
import {
  MODEL_MAPPING_PREVIEW_FALLBACK,
  formatModelNames,
  getModelMappingGuardrail,
} from '../channel-editor-state'

export type ChannelModelPrefillGroup = {
  id: number
  name: string
  items: string | string[]
}

type ChannelModelsSectionProps = {
  allModels: string[]
  basicModels: string[]
  groupOptions: Option[]
  isLoadingGroups: boolean
  isSubmitting: boolean
  onFetchModels: () => void
  prefillGroups: ChannelModelPrefillGroup[]
}

export function ChannelModelsSection(props: ChannelModelsSectionProps) {
  const { t } = useTranslation()
  const form = useFormContext<ChannelFormValues>()
  const { copyToClipboard } = useCopyToClipboard()
  const currentType = useWatch({ control: form.control, name: 'type' })
  const currentModels = useWatch({ control: form.control, name: 'models' })
  const currentModelMapping = useWatch({
    control: form.control,
    name: 'model_mapping',
  })

  const currentModelsArray = useMemo(
    () => parseModelsString(currentModels),
    [currentModels]
  )
  const modelOptions = useMemo(() => {
    const models = new Set([...props.allModels, ...currentModelsArray])
    return Array.from(models).map((model) => ({ value: model, label: model }))
  }, [props.allModels, currentModelsArray])
  const modelMappingGuardrail = useMemo(
    () => getModelMappingGuardrail(currentModelMapping, currentModelsArray),
    [currentModelMapping, currentModelsArray]
  )
  const mappingPreviewPairs =
    modelMappingGuardrail.entries.length > 0
      ? modelMappingGuardrail.entries.slice(0, 3)
      : MODEL_MAPPING_PREVIEW_FALLBACK
  const remainingMappingCount = Math.max(
    modelMappingGuardrail.entries.length - mappingPreviewPairs.length,
    0
  )

  const updateModels = useCallback(
    (newModels: string[], merge = false) => {
      const finalModels = merge
        ? formatModelsArray([...currentModelsArray, ...newModels])
        : formatModelsArray(newModels)
      form.setValue('models', finalModels, {
        shouldDirty: true,
        shouldValidate: true,
      })
      return newModels.length
    },
    [currentModelsArray, form]
  )

  const handleAddPrefillGroup = (group: ChannelModelPrefillGroup) => {
    try {
      const items = Array.isArray(group.items)
        ? group.items
        : JSON.parse(group.items)
      if (!Array.isArray(items)) throw new Error('Invalid items format')

      const count = updateModels(items, true)
      toast.success(
        t('Added {{count}} models from "{{name}}"', {
          count,
          name: group.name,
        })
      )
    } catch {
      toast.error(t('Failed to parse group items'))
    }
  }

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Models & Groups')}
        description={t('Published models, groups, and model remapping rules.')}
        icon={<Boxes className='h-4 w-4' aria-hidden='true' />}
      />

      <div className='space-y-5'>
        <div className='border-border/60 bg-muted/10 rounded-lg border p-4'>
          <FormField
            control={form.control}
            name='models'
            render={() => (
              <FormItem className='space-y-3'>
                <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                  <div className='space-y-1'>
                    <FormLabel>{t('Models *')}</FormLabel>
                    <FormDescription>
                      {t(FIELD_DESCRIPTIONS.MODELS)}
                    </FormDescription>
                  </div>
                  <Badge variant='outline' className='w-fit'>
                    {t('Selected {{count}}', {
                      count: currentModelsArray.length,
                    })}
                  </Badge>
                </div>
                <FormControl>
                  <MultiSelect
                    options={modelOptions}
                    selected={currentModelsArray}
                    onChange={updateModels}
                    placeholder={t('Select models or add custom ones')}
                    allowCreate
                    createLabel='Add custom model "{{value}}"'
                    maxVisibleChips={6}
                  />
                </FormControl>
                {modelMappingGuardrail.exposedTargetModels.length > 0 && (
                  <Alert className='border-warning/30 bg-warning/5 text-foreground'>
                    <AlertDescription className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                      <span className='min-w-0 [overflow-wrap:anywhere]'>
                        {t('The mapped upstream model(s)')}{' '}
                        {formatModelNames(
                          modelMappingGuardrail.exposedTargetModels
                        )}{' '}
                        {t(
                          'are also listed here. Remove them from Models to keep the `/v1/models` response user-friendly and hide vendor-specific names.'
                        )}
                      </span>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        className='shrink-0'
                        onClick={() => {
                          const hiddenTargets = new Set(
                            modelMappingGuardrail.exposedTargetModels
                          )
                          updateModels(
                            currentModelsArray.filter(
                              (model) => !hiddenTargets.has(model)
                            )
                          )
                        }}
                      >
                        {t('Remove mapped targets')}
                      </Button>
                    </AlertDescription>
                  </Alert>
                )}
                <FormMessage />
              </FormItem>
            )}
          />

          <Separator className='my-4' />

          <div className='space-y-3'>
            <div>
              <p className='text-sm font-medium'>{t('Quick actions')}</p>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Use presets or upstream discovery to populate the model list faster.'
                )}
              </p>
            </div>
            <div className='flex flex-wrap gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => {
                  updateModels(props.basicModels)
                  toast.success(
                    t('Filled {{count}} related model(s)', {
                      count: props.basicModels.length,
                    })
                  )
                }}
                disabled={!props.basicModels.length}
              >
                <FileText className='mr-2 h-4 w-4' aria-hidden='true' />
                {t('Fill Related Models')}
              </Button>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => {
                  updateModels(props.allModels)
                  toast.success(
                    t('Filled {{count}} model(s)', {
                      count: props.allModels.length,
                    })
                  )
                }}
                disabled={!props.allModels.length}
              >
                <Plus className='mr-2 h-4 w-4' aria-hidden='true' />
                {t('Fill All Models')}
              </Button>
              {MODEL_FETCHABLE_TYPES.has(currentType) && (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={props.onFetchModels}
                >
                  <Sparkles className='mr-2 h-4 w-4' aria-hidden='true' />
                  {t('Fetch from Upstream')}
                </Button>
              )}
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => copyToClipboard(currentModels || '')}
                disabled={currentModelsArray.length === 0}
              >
                <Copy className='mr-2 h-4 w-4' aria-hidden='true' />
                {t('Copy All')}
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => {
                  updateModels([])
                  toast.success(t('Cleared all models'))
                }}
                disabled={currentModelsArray.length === 0}
              >
                <Eraser className='mr-2 h-4 w-4' aria-hidden='true' />
                {t('Clear All')}
              </Button>
            </div>
            {props.prefillGroups.length > 0 && (
              <div className='flex flex-wrap items-center gap-2'>
                <span className='text-muted-foreground text-xs'>
                  {t('Preset groups')}:
                </span>
                {props.prefillGroups.map((group) => (
                  <Button
                    key={group.id}
                    type='button'
                    variant='secondary'
                    size='sm'
                    onClick={() => handleAddPrefillGroup(group)}
                  >
                    {group.name}
                  </Button>
                ))}
              </div>
            )}
          </div>
        </div>

        <div className='border-border/60 rounded-lg border p-4'>
          <FormField
            control={form.control}
            name='model_mapping'
            render={({ field }) => (
              <FormItem className='space-y-3'>
                <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
                  <div className='space-y-1'>
                    <div className='flex items-center gap-2'>
                      <FormLabel className='mb-0'>
                        {t('Model Mapping')}
                      </FormLabel>
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <Button
                              type='button'
                              variant='ghost'
                              size='icon-sm'
                              className='text-muted-foreground hover:text-foreground size-auto p-0'
                              aria-label={t('How model mapping works')}
                            />
                          }
                        >
                          <HelpCircle className='h-4 w-4' aria-hidden='true' />
                        </TooltipTrigger>
                        <TooltipContent
                          side='top'
                          align='start'
                          className='max-w-xs space-y-2 text-left'
                        >
                          <p className='text-xs font-semibold uppercase'>
                            {t('Request flow')}
                          </p>
                          <div className='space-y-1 font-mono text-xs'>
                            {mappingPreviewPairs.map((pair) => (
                              <div
                                key={`${pair.source}-${pair.target}`}
                                className='flex min-w-0 items-center gap-1'
                              >
                                <span className='min-w-0 [overflow-wrap:anywhere]'>
                                  {pair.source}
                                </span>
                                <ArrowRight
                                  className='h-3.5 w-3.5 opacity-70'
                                  aria-hidden='true'
                                />
                                <span className='min-w-0 [overflow-wrap:anywhere]'>
                                  {pair.target}
                                </span>
                              </div>
                            ))}
                            {remainingMappingCount > 0 && (
                              <div className='text-[11px] opacity-70'>
                                +{remainingMappingCount} {t('more mapping')}
                                {remainingMappingCount > 1 ? 's' : ''}
                              </div>
                            )}
                          </div>
                          <p className='text-[11px] leading-relaxed opacity-80'>
                            {t(
                              'Users call the model on the left. The platform forwards the request to the upstream model on the right.'
                            )}
                          </p>
                        </TooltipContent>
                      </Tooltip>
                    </div>
                    <FormDescription>
                      {t(FIELD_DESCRIPTIONS.MODEL_MAPPING)}
                    </FormDescription>
                  </div>
                </div>
                <FormControl>
                  <ModelMappingEditor
                    value={field.value || ''}
                    onChange={field.onChange}
                    disabled={props.isSubmitting}
                    sourceModelOptions={currentModelsArray}
                    targetModelOptions={modelOptions.map(
                      (option) => option.value
                    )}
                  />
                </FormControl>
                {modelMappingGuardrail.invalidJson && (
                  <Alert variant='destructive'>
                    <AlertDescription>
                      {t('Model Mapping must be a JSON object like')}{' '}
                      <code className='font-mono'>
                        {'{"gpt-4":"Azure-GPT4"}'}
                      </code>
                      {t('. Please fix the JSON before saving.')}
                    </AlertDescription>
                  </Alert>
                )}
                {modelMappingGuardrail.missingSourceModels.length > 0 && (
                  <Alert className='border-warning/30 bg-warning/5 text-foreground'>
                    <AlertDescription className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                      <span className='min-w-0 [overflow-wrap:anywhere]'>
                        {t('Add')}{' '}
                        {formatModelNames(
                          modelMappingGuardrail.missingSourceModels
                        )}{' '}
                        {t(
                          'to the Models list so users can use them before the mapping sends traffic upstream.'
                        )}
                      </span>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        className='shrink-0'
                        onClick={() =>
                          updateModels(
                            modelMappingGuardrail.missingSourceModels,
                            true
                          )
                        }
                      >
                        {t('Add missing models')}
                      </Button>
                    </AlertDescription>
                  </Alert>
                )}
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <div className='border-border/60 rounded-lg border p-4'>
          <FormField
            control={form.control}
            name='group'
            render={({ field }) => (
              <FormItem className='space-y-3'>
                <div className='space-y-1'>
                  <FormLabel>{t('Groups *')}</FormLabel>
                  <FormDescription>
                    {t(FIELD_DESCRIPTIONS.GROUP)}
                  </FormDescription>
                </div>
                <FormControl>
                  {props.isLoadingGroups ? (
                    <Skeleton className='h-10 w-full' />
                  ) : (
                    <MultiSelect
                      options={props.groupOptions}
                      selected={field.value}
                      onChange={field.onChange}
                      placeholder={t(FIELD_PLACEHOLDERS.GROUP)}
                    />
                  )}
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      </div>
    </SideDrawerSection>
  )
}
