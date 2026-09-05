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
import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Separator } from '@/components/ui/separator'
import { previewH3Billing } from '../api'
import {
  SettingsForm,
  SettingsFormGrid,
  SettingsFormGridItem,
  SettingsControlGroup,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'
import {
  buildH3BillingFormValues,
  buildH3BillingProfile,
  DEFAULT_H3_BILLING_PROFILE,
  H3_BILLING_PROFILE_KEY,
  fingerprintH3BillingProfile,
  hasLegacyH3RateCard,
  parseH3BillingProfiles,
  serializeH3BillingProfiles,
  type H3BillingPreview,
  type H3BillingPreviewScenario,
  type H3BillingProfile,
  type H3BillingProfiles,
  type H3Resolution,
} from './h3-billing-utils'

const createH3BillingSchema = (t: (key: string) => string) => {
  const decimalString = z
    .string()
    .trim()
    .min(1, t('Enter a non-negative decimal price.'))
    .refine(
      (value) => /^\d+(?:\.\d+)?$/.test(value),
      t('Enter a non-negative decimal price.')
    )

  return z.object({
    output768Price: decimalString,
    output2KPrice: decimalString,
    inputVideo768Price: decimalString,
    inputVideo2KPrice: decimalString,
    inputVideoMaxSeconds: z.number().int().min(1).max(15),
    inputImageFreeCount: z.number().int().min(0).max(9),
    inputImageExtraPrice: decimalString,
  })
}

type H3BillingFormValues = z.infer<ReturnType<typeof createH3BillingSchema>>

function getObservedTimestamp() {
  return Date.now()
}

type H3BillingSettingsProps = {
  defaultValue: string
  legacyRateCards: string
}

type PreviewScenarioState = {
  resolution: H3Resolution
  outputDurationSeconds: number
  inputVideoCount: number
  inputAudioCount: number
  inputImageCount: number
  includeActual: boolean
  actualOutputDurationMs: number
  actualInputVideoDurationMs: number
  actualInputAudioDurationMs: number
}

const DEFAULT_SCENARIO: PreviewScenarioState = {
  resolution: '768P',
  outputDurationSeconds: 5,
  inputVideoCount: 0,
  inputAudioCount: 0,
  inputImageCount: 0,
  includeActual: false,
  actualOutputDurationMs: 5000,
  actualInputVideoDurationMs: 0,
  actualInputAudioDurationMs: 0,
}

function buildDefaultValues(profile: H3BillingProfile): H3BillingFormValues {
  return buildH3BillingFormValues(profile)
}

function buildPreviewScenario(
  state: PreviewScenarioState
): H3BillingPreviewScenario {
  const scenario: H3BillingPreviewScenario = {
    resolution: state.resolution,
    outputDurationSeconds: state.outputDurationSeconds,
    inputVideoCount: state.inputVideoCount,
    inputAudioCount: state.inputAudioCount,
    inputImageCount: state.inputImageCount,
  }
  if (state.includeActual) {
    scenario.actual = {
      outputDurationMs: state.actualOutputDurationMs,
      inputImageCount: state.inputImageCount,
      ...(state.inputVideoCount > 0
        ? { inputVideoDurationMs: state.actualInputVideoDurationMs }
        : {}),
      ...(state.inputAudioCount > 0
        ? { inputAudioDurationMs: state.actualInputAudioDurationMs }
        : {}),
    }
  }
  return scenario
}

function PreviewSummary({ preview }: { preview: H3BillingPreview }) {
  const { t } = useTranslation()
  const adjustment = preview.adjustment_quota
  return (
    <div className='space-y-3'>
      <div className='grid gap-3 sm:grid-cols-3'>
        {[
          { label: t('Estimate'), quote: preview.estimate },
          { label: t('Reserve'), quote: preview.reserve },
          { label: t('Actual'), quote: preview.final },
        ].map((item) => (
          <div key={item.label} className='min-w-0 rounded-lg border px-3 py-2'>
            <div className='text-muted-foreground text-xs'>{item.label}</div>
            {item.quote ? (
              <div className='mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1'>
                <strong className='font-mono text-sm'>
                  {item.quote.price}
                </strong>
                <span className='text-muted-foreground text-xs'>
                  {item.quote.quota.toLocaleString()} {t('quota')}
                </span>
              </div>
            ) : (
              <div className='text-muted-foreground mt-1 text-sm'>
                {t('Not included')}
              </div>
            )}
          </div>
        ))}
      </div>
      {adjustment !== undefined && (
        <p className='text-muted-foreground text-xs'>
          {adjustment < 0
            ? t('Estimated refund: {{count}} quota', {
                count: preview.refund_quota ?? Math.abs(adjustment),
              })
            : t('Additional quota required: {{count}}', { count: adjustment })}
        </p>
      )}
      <p className='text-muted-foreground font-mono text-xs break-all'>
        {t('Configuration hash')}: {preview.config_hash}
      </p>
    </div>
  )
}

export function H3BillingSettings(props: H3BillingSettingsProps) {
  const { t, i18n } = useTranslation()
  const updateOption = useUpdateOption()
  const previewMutation = useMutation({ mutationFn: previewH3Billing })
  const h3BillingSchema = useMemo(() => createH3BillingSchema(t), [t])
  const parsed = useMemo(
    () => parseH3BillingProfiles(props.defaultValue || '{}'),
    [props.defaultValue]
  )
  const profile =
    parsed.profiles[H3_BILLING_PROFILE_KEY] ?? DEFAULT_H3_BILLING_PROFILE
  const formDefaults = useMemo(() => buildDefaultValues(profile), [profile])
  const form = useForm<H3BillingFormValues>({
    resolver: zodResolver(h3BillingSchema),
    mode: 'onChange',
    defaultValues: formDefaults,
  })
  useResetForm(form, formDefaults)

  const [scenario, setScenario] = useState(DEFAULT_SCENARIO)
  const [groupRatio, setGroupRatio] = useState(1)
  const [preview, setPreview] = useState<{
    data: H3BillingPreview
    fingerprint: string
  } | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [lastSaved, setLastSaved] = useState<{
    configHash: string
    observedAt: number
    persistedValue: string
  } | null>(null)
  const previewRequestId = useRef(0)
  const sourceValueRef = useRef(props.defaultValue)

  const invalidatePreview = useCallback(() => {
    previewRequestId.current += 1
    setPreview(null)
    setPreviewError('')
  }, [])
  const updateScenario = useCallback(
    <K extends keyof PreviewScenarioState>(
      key: K,
      value: PreviewScenarioState[K]
    ) => {
      setScenario((current) => ({ ...current, [key]: value }))
      invalidatePreview()
    },
    [invalidatePreview]
  )

  useEffect(() => {
    if (sourceValueRef.current === props.defaultValue) return
    sourceValueRef.current = props.defaultValue
    invalidatePreview()
    setLastSaved((current) =>
      current?.persistedValue === props.defaultValue ? current : null
    )
  }, [props.defaultValue, invalidatePreview])

  const profileError = Boolean(parsed.error)
  const legacyRateCard = useMemo(
    () => hasLegacyH3RateCard(props.legacyRateCards),
    [props.legacyRateCards]
  )

  const getDraftProfile = useCallback(
    (values: H3BillingFormValues) => buildH3BillingProfile(profile, values),
    [profile]
  )

  const handlePreview = async () => {
    const valid = await form.trigger()
    if (!valid || profileError) return
    setPreviewError('')
    const values = form.getValues()
    const draftProfile = getDraftProfile(values)
    const requestId = ++previewRequestId.current
    try {
      const response = await previewMutation.mutateAsync({
        profile: draftProfile,
        scenario: buildPreviewScenario(scenario),
        groupRatio:
          Number.isFinite(groupRatio) && groupRatio >= 0 ? groupRatio : 0,
      })
      if (requestId !== previewRequestId.current) return
      if (!response.success || !response.data) {
        const message = response.message || t('H3 billing preview failed')
        setPreviewError(message)
        toast.error(message)
        return
      }
      setPreview({
        data: response.data,
        fingerprint: fingerprintH3BillingProfile(draftProfile),
      })
    } catch (error) {
      if (requestId !== previewRequestId.current) return
      const message =
        error instanceof Error ? error.message : t('H3 billing preview failed')
      setPreviewError(message)
      toast.error(message)
    }
  }

  const handleSave = async (values: H3BillingFormValues) => {
    if (profileError) return
    const draftProfile = getDraftProfile(values)
    const fingerprint = fingerprintH3BillingProfile(draftProfile)
    if (!preview || preview.fingerprint !== fingerprint) {
      return
    }
    const profiles: H3BillingProfiles = Object.fromEntries(
      Object.entries(parsed.profiles).map(([key, value]) => [key, { ...value }])
    )
    profiles[H3_BILLING_PROFILE_KEY] = draftProfile
    const persistedValue = serializeH3BillingProfiles(profiles)
    const response = await updateOption.mutateAsync({
      key: 'task_billing_setting.h3_profiles',
      value: persistedValue,
    })
    if (response.success) {
      setLastSaved({
        configHash: preview.data.config_hash,
        observedAt: getObservedTimestamp(),
        persistedValue,
      })
      setPreview(null)
    }
  }

  return (
    <SettingsSection title={t('MiniMax-H3 Billing')}>
      <div className='space-y-4'>
        <Alert>
          <AlertTitle>{t('Structured H3 billing profile')}</AlertTitle>
          <AlertDescription>
            {t(
              'Configure the bounded-actual prices used by new MiniMax-H3 tasks. Existing tasks keep their submitted billing snapshot.'
            )}
          </AlertDescription>
        </Alert>

        {legacyRateCard && (
          <Alert variant='destructive'>
            <AlertTitle>{t('Legacy rate card detected')}</AlertTitle>
            <AlertDescription>
              {t(
                'The legacy MiniMax-H3 rate card is kept for compatibility and is not this structured H3 profile.'
              )}
            </AlertDescription>
          </Alert>
        )}

        {profileError && (
          <Alert variant='destructive'>
            <AlertTitle>{t('H3 profile is unavailable')}</AlertTitle>
            <AlertDescription>
              {t(
                'The saved H3 profile is incomplete or invalid. Saving is disabled until the server returns a valid profile.'
              )}
            </AlertDescription>
          </Alert>
        )}

        <Form {...form}>
          <SettingsForm onSubmit={form.handleSubmit(handleSave)}>
            <SettingsControlGroup>
              <div className='flex flex-wrap items-start justify-between gap-3'>
                <div className='min-w-0'>
                  <h3 className='text-sm font-medium'>
                    {t('Price per second')}
                  </h3>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {t(
                      'Prices are entered in the configured currency and validated again by the server.'
                    )}
                  </p>
                </div>
                <div className='text-muted-foreground max-w-full text-right text-xs'>
                  <div>{t('Model')}: MiniMax-H3</div>
                  <div>{t('Channel')}: 35</div>
                </div>
              </div>
              <SettingsFormGrid>
                {[
                  ['output768Price', t('Output video / 768P')],
                  ['output2KPrice', t('Output video / 2K')],
                  ['inputVideo768Price', t('Input video / 768P')],
                  ['inputVideo2KPrice', t('Input video / 2K')],
                ].map(([name, label]) => (
                  <FormField
                    key={name}
                    control={form.control}
                    name={name as keyof H3BillingFormValues}
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{label}</FormLabel>
                        <FormControl>
                          <Input
                            inputMode='decimal'
                            {...field}
                            onChange={(event) => {
                              field.onChange(event)
                              invalidatePreview()
                            }}
                          />
                        </FormControl>
                        <FormDescription>{profile.currency}</FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                ))}
              </SettingsFormGrid>
            </SettingsControlGroup>

            <SettingsControlGroup>
              <h3 className='text-sm font-medium'>
                {t('Input limits and free allowance')}
              </h3>
              <SettingsFormGrid>
                <FormField
                  control={form.control}
                  name='inputVideoMaxSeconds'
                  render={({ field }) => {
                    const numeric = safeNumberFieldProps(field)
                    return (
                      <FormItem>
                        <FormLabel>
                          {t('Input video reservation cap')}
                        </FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={15}
                            step={1}
                            {...numeric}
                            onChange={(event) => {
                              numeric.onChange(event)
                              invalidatePreview()
                            }}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('1-15 seconds, shared across input videos')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )
                  }}
                />
                <FormField
                  control={form.control}
                  name='inputImageFreeCount'
                  render={({ field }) => {
                    const numeric = safeNumberFieldProps(field)
                    return (
                      <FormItem>
                        <FormLabel>{t('Free input images')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            max={9}
                            step={1}
                            {...numeric}
                            onChange={(event) => {
                              numeric.onChange(event)
                              invalidatePreview()
                            }}
                          />
                        </FormControl>
                        <FormDescription>{t('0-9 images')}</FormDescription>
                        <FormMessage />
                      </FormItem>
                    )
                  }}
                />
                <FormField
                  control={form.control}
                  name='inputImageExtraPrice'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Extra input image price')}</FormLabel>
                      <FormControl>
                        <Input
                          inputMode='decimal'
                          {...field}
                          onChange={(event) => {
                            field.onChange(event)
                            invalidatePreview()
                          }}
                        />
                      </FormControl>
                      <FormDescription>
                        {profile.currency} / image
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormItem>
                  <FormLabel>{t('Input audio price')}</FormLabel>
                  <Input value='0' readOnly disabled />
                  <FormDescription>
                    {t('Free in schema version 1')}
                  </FormDescription>
                </FormItem>
              </SettingsFormGrid>
            </SettingsControlGroup>

            <SettingsControlGroup>
              <div>
                <h3 className='text-sm font-medium'>
                  {t('Server-side billing preview')}
                </h3>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'Preview uses the draft profile and has no task, wallet, token, settlement, log, or statistics side effects.'
                  )}
                </p>
              </div>
              <SettingsFormGrid>
                <SettingsFormGridItem>
                  <label className='flex flex-col gap-2 text-sm font-medium'>
                    {t('Resolution')}
                    <NativeSelect
                      value={scenario.resolution}
                      onChange={(event) =>
                        updateScenario(
                          'resolution',
                          event.target.value as H3Resolution
                        )
                      }
                    >
                      <NativeSelectOption value='768P'>768P</NativeSelectOption>
                      <NativeSelectOption value='2K'>2K</NativeSelectOption>
                    </NativeSelect>
                  </label>
                </SettingsFormGridItem>
                <PreviewNumberField
                  label={t('Requested output seconds')}
                  value={scenario.outputDurationSeconds}
                  min={4}
                  max={15}
                  onChange={(value) =>
                    updateScenario('outputDurationSeconds', value)
                  }
                />
                <PreviewNumberField
                  label={t('Input video count')}
                  value={scenario.inputVideoCount}
                  min={0}
                  max={3}
                  onChange={(value) => updateScenario('inputVideoCount', value)}
                />
                <PreviewNumberField
                  label={t('Input audio count')}
                  value={scenario.inputAudioCount}
                  min={0}
                  max={3}
                  onChange={(value) => updateScenario('inputAudioCount', value)}
                />
                <PreviewNumberField
                  label={t('Input image count')}
                  value={scenario.inputImageCount}
                  min={0}
                  max={9}
                  onChange={(value) => updateScenario('inputImageCount', value)}
                />
                <div className='flex min-w-0 flex-col gap-2'>
                  <label
                    htmlFor='h3-preview-group-ratio'
                    className='text-sm font-medium'
                  >
                    {t('Preview group ratio')}
                  </label>
                  <Input
                    id='h3-preview-group-ratio'
                    type='number'
                    min={0}
                    step={0.01}
                    value={groupRatio}
                    onChange={(event) => {
                      setGroupRatio(Number(event.target.value) || 0)
                      invalidatePreview()
                    }}
                  />
                </div>
              </SettingsFormGrid>

              <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-3'>
                <label className='flex min-w-0 items-center gap-2 text-sm'>
                  <Checkbox
                    checked={scenario.includeActual}
                    onCheckedChange={(checked) =>
                      updateScenario('includeActual', checked === true)
                    }
                  />
                  <span>{t('Include actual usage and refund preview')}</span>
                </label>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => void handlePreview()}
                  disabled={
                    previewMutation.isPending ||
                    !form.formState.isValid ||
                    profileError
                  }
                >
                  {previewMutation.isPending
                    ? t('Previewing...')
                    : t('Preview billing')}
                </Button>
              </div>

              {scenario.includeActual && (
                <div className='grid gap-3 border-t pt-3 sm:grid-cols-3'>
                  <PreviewNumberField
                    label={t('Actual output milliseconds')}
                    value={scenario.actualOutputDurationMs}
                    min={4000}
                    max={15000}
                    onChange={(value) =>
                      updateScenario('actualOutputDurationMs', value)
                    }
                  />
                  <PreviewNumberField
                    label={t('Actual input video milliseconds')}
                    value={scenario.actualInputVideoDurationMs}
                    min={0}
                    max={15000}
                    onChange={(value) =>
                      updateScenario('actualInputVideoDurationMs', value)
                    }
                  />
                  <PreviewNumberField
                    label={t('Actual input audio milliseconds')}
                    value={scenario.actualInputAudioDurationMs}
                    min={0}
                    max={15000}
                    onChange={(value) =>
                      updateScenario('actualInputAudioDurationMs', value)
                    }
                  />
                </div>
              )}

              {preview && (
                <>
                  <Separator />
                  <PreviewSummary preview={preview.data} />
                </>
              )}
              {previewError && (
                <p className='text-destructive text-sm'>{previewError}</p>
              )}
            </SettingsControlGroup>

            {lastSaved && (
              <Alert>
                <AlertTitle>{t('Saved successfully')}</AlertTitle>
                <AlertDescription className='space-y-1'>
                  <div className='font-mono text-xs break-all'>
                    {t('Configuration hash')}: {lastSaved.configHash}
                  </div>
                  <div className='text-xs'>
                    {t('Last updated:')}{' '}
                    {new Date(lastSaved.observedAt).toLocaleString(
                      i18n.resolvedLanguage ?? i18n.language
                    )}
                  </div>
                </AlertDescription>
              </Alert>
            )}

            <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs'>
              <span>{t('Schema')}: 1</span>
              <span>{t('Mode')}: bounded_actual</span>
              <span>
                {t('Rule key')}: {H3_BILLING_PROFILE_KEY}
              </span>
              <span>
                {t('Audio')}: {t('Free')}
              </span>
            </div>

            <SettingsPageFormActions
              onSave={form.handleSubmit(handleSave)}
              isSaving={updateOption.isPending}
              isSaveDisabled={
                profileError ||
                !form.formState.isValid ||
                !preview ||
                preview.fingerprint !==
                  fingerprintH3BillingProfile(getDraftProfile(form.getValues()))
              }
              saveLabel='Save H3 billing profile'
            />
          </SettingsForm>
        </Form>
      </div>
    </SettingsSection>
  )
}

type PreviewNumberFieldProps = {
  label: string
  value: number
  min: number
  max: number
  onChange: (value: number) => void
}

function PreviewNumberField(props: PreviewNumberFieldProps) {
  const id = useId()
  return (
    <div className='flex min-w-0 flex-col gap-2'>
      <label htmlFor={id} className='text-sm font-medium'>
        {props.label}
      </label>
      <Input
        id={id}
        type='number'
        min={props.min}
        max={props.max}
        step={1}
        value={props.value}
        onChange={(event) => props.onChange(Number(event.target.value) || 0)}
      />
    </div>
  )
}
