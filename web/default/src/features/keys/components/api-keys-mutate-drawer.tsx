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
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm, type SubmitErrorHandler } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, KeyRound, Settings2, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getUserModels, getUserGroups } from '@/lib/api'
import { DEFAULT_AUTO_ROUTE_KEY } from '@/lib/auto-routes'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { handleServerError } from '@/lib/handle-server-error'
import { wasSecureVerificationErrorReported } from '@/lib/secure-verification'
import { cn } from '@/lib/utils'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { DateTimePicker } from '@/components/datetime-picker'
import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
import { MultiSelect } from '@/components/multi-select'
import { createApiKey, updateApiKey, getApiKey } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import {
  getApiKeyFormSchema,
  MAX_MANUAL_ROUTING_GROUPS,
  shouldIncludeRoutingProjection,
  type ApiKeyFormValues,
  getApiKeyFormDefaultValues,
  transformFormDataToPayload,
  transformApiKeyToFormDefaults,
} from '../lib'
import { type ApiKey } from '../types'
import { type ApiKeyGroupOption } from './api-key-group-combobox'
import {
  ApiKeyRoutingEditor,
  type ApiKeyAutoRouteOption,
} from './api-key-routing-editor'
import { useApiKeys } from './api-keys-provider'

type ApiKeyMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: ApiKey
}

export function ApiKeysMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: ApiKeyMutateDrawerProps) {
  const { t } = useTranslation()
  const isUpdate = !!currentRow
  const { triggerRefresh, withApiTokenVerification } = useApiKeys()
  const { status } = useStatus()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [editingLegacyRouting, setEditingLegacyRouting] = useState(false)
  const [preservedSmartRoute, setPreservedSmartRoute] = useState<string>()
  const [preservedManualGroups, setPreservedManualGroups] = useState<string[]>(
    []
  )
  const currentRowId = currentRow?.id
  const defaultAutoRoute =
    typeof status?.default_auto_route === 'string'
      ? status.default_auto_route
      : DEFAULT_AUTO_ROUTE_KEY

  // Fetch models
  const { data: modelsData } = useQuery({
    queryKey: ['user-models'],
    queryFn: getUserModels,
    staleTime: 5 * 60 * 1000, // Cache for 5 minutes
  })

  // Fetch groups
  const { data: groupsData, isLoading: groupsLoading } = useQuery({
    queryKey: ['user-groups'],
    queryFn: getUserGroups,
    staleTime: 5 * 60 * 1000,
  })

  const models = modelsData?.data || []
  const groupsRaw = useMemo(() => groupsData?.data || {}, [groupsData?.data])
  const {
    realGroups,
    autoRouteOptions,
    effectiveAutoRoute,
    defaultManualGroups,
  } = useMemo(() => {
    const entries = Object.entries(groupsRaw)
    const real: ApiKeyGroupOption[] = entries
      .filter(([, info]) => info.auto !== true)
      .map(([key, info]) => ({
        value: key,
        label: key,
        desc: info.desc || key,
        ratio: info.ratio,
      }))
    const realGroupNames = new Set(real.map((option) => option.value))
    const serverRoutes = groupsData?.auto_routes || []
    const routes: ApiKeyAutoRouteOption[] = (
      serverRoutes.length > 0
        ? serverRoutes.map((route) => ({
            value: route.key,
            label: route.name || route.key,
            groups: route.groups,
          }))
        : entries
            .filter(([, info]) => info.auto === true)
            .map(([key, info]) => ({
              value: key,
              label: info.desc || key,
              groups: info.groups || [],
            }))
    )
      .map((route) => ({
        ...route,
        groups: route.groups.filter((group) => realGroupNames.has(group)),
      }))
      .filter((route) => route.groups.length > 0)
    const route = routes.some((option) => option.value === defaultAutoRoute)
      ? defaultAutoRoute
      : routes[0]?.value || ''
    const routeGroups =
      routes
        .find((option) => option.value === route)
        ?.groups.slice(0, MAX_MANUAL_ROUTING_GROUPS) || []
    return {
      realGroups: real,
      autoRouteOptions: routes,
      effectiveAutoRoute: route,
      defaultManualGroups:
        routeGroups.length > 0
          ? routeGroups
          : real.slice(0, 1).map((option) => option.value),
    }
  }, [defaultAutoRoute, groupsData?.auto_routes, groupsRaw])

  const routingContextRef = useRef({
    autoRouteOptions,
    realGroups,
    defaultManualGroups,
    effectiveAutoRoute,
  })
  routingContextRef.current = {
    autoRouteOptions,
    realGroups,
    defaultManualGroups,
    effectiveAutoRoute,
  }
  const schema = useMemo(
    () =>
      getApiKeyFormSchema(t, {
        smartRoutes: autoRouteOptions.map((route) => route.value),
        manualGroups: realGroups.map((group) => group.value),
        preservedSmartRoute,
        preservedManualGroups,
      }),
    [
      autoRouteOptions,
      preservedManualGroups,
      preservedSmartRoute,
      realGroups,
      t,
    ]
  )

  const form = useForm<ApiKeyFormValues>({
    resolver: zodResolver(schema),
    defaultValues: getApiKeyFormDefaultValues(defaultAutoRoute),
  })

  // Load existing data when updating
  useEffect(() => {
    if (!open || groupsLoading) return

    let cancelled = false
    const {
      autoRouteOptions: availableAutoRoutes,
      realGroups: availableRealGroups,
      defaultManualGroups: availableDefaultManualGroups,
      effectiveAutoRoute: availableAutoRoute,
    } = routingContextRef.current

    if (isUpdate && currentRowId != null) {
      setEditingLegacyRouting(false)
      setPreservedSmartRoute(undefined)
      setPreservedManualGroups([])
      getApiKey(currentRowId)
        .then((result) => {
          if (cancelled) return
          if (!result.success || !result.data) {
            toast.error(result.message || t(ERROR_MESSAGES.UNEXPECTED))
            return
          }

          setEditingLegacyRouting(Boolean(result.data.routing_legacy))
          const routing = result.data.routing
          const routeKey = routing?.route || result.data.group
          const selectableAutoRoutes = new Set(
            availableAutoRoutes.map((route) => route.value)
          )
          const selectableManualGroups = new Set(
            availableRealGroups.map((group) => group.value)
          )
          const unavailableSmartRoute =
            routing?.mode === 'smart' &&
            routeKey &&
            !selectableAutoRoutes.has(routeKey)
          const unavailableManualGroups =
            routing?.mode === 'manual'
              ? (routing.groups || []).filter(
                  (group) => !selectableManualGroups.has(group)
                )
              : []
          setPreservedSmartRoute(unavailableSmartRoute ? routeKey : undefined)
          setPreservedManualGroups(unavailableManualGroups)
          const routeManualGroups =
            availableAutoRoutes.find((route) => route.value === routeKey)
              ?.groups || availableDefaultManualGroups
          form.reset(
            transformApiKeyToFormDefaults(result.data, routeManualGroups)
          )
        })
        .catch((error) => {
          if (!cancelled) {
            handleServerError(error, {
              fallback: t(ERROR_MESSAGES.UNEXPECTED),
            })
          }
        })
    } else if (!isUpdate) {
      setEditingLegacyRouting(false)
      setPreservedSmartRoute(undefined)
      setPreservedManualGroups([])
      form.reset(
        getApiKeyFormDefaultValues(
          availableAutoRoute,
          availableDefaultManualGroups
        )
      )
    }

    return () => {
      cancelled = true
    }
  }, [open, isUpdate, currentRowId, form, groupsLoading, t])

  const onSubmit = async (data: ApiKeyFormValues) => {
    setIsSubmitting(true)
    try {
      const dirtyFields = form.formState.dirtyFields
      const routingChanged = Boolean(
        dirtyFields.routing_mode ||
        dirtyFields.routing_route ||
        dirtyFields.manual_groups ||
        dirtyFields.cross_group_retry
      )
      const basePayload = transformFormDataToPayload(data, {
        includeRouting: shouldIncludeRoutingProjection(
          isUpdate,
          editingLegacyRouting ||
            Boolean(preservedSmartRoute) ||
            preservedManualGroups.length > 0,
          routingChanged
        ),
      })

      if (isUpdate && currentRow) {
        const result = await updateApiKey({
          ...basePayload,
          id: currentRow.id,
        })
        if (result.success) {
          toast.success(t(SUCCESS_MESSAGES.API_KEY_UPDATED))
          onOpenChange(false)
          triggerRefresh()
        } else {
          toast.error(result.message || t(ERROR_MESSAGES.UPDATE_FAILED))
        }
      } else {
        // Create mode - handle batch creation
        const count = data.tokenCount || 1
        let created = 0
        await withApiTokenVerification(async (): Promise<number> => {
          for (let i = created; i < count; i++) {
            const result = await createApiKey({
              ...basePayload,
              name:
                i === 0 && data.name
                  ? data.name
                  : `${data.name || 'default'}-${Math.random().toString(36).slice(2, 8)}`,
            })
            if (result.success) {
              created++
            } else {
              toast.error(result.message || t(ERROR_MESSAGES.CREATE_FAILED))
              break
            }
          }

          return created
        })

        if (created > 0) {
          toast.success(
            t('Successfully created {{count}} API Key(s)', {
              count: created,
            })
          )
          onOpenChange(false)
          triggerRefresh()
        }
      }
    } catch (error) {
      if (!wasSecureVerificationErrorReported(error)) {
        handleServerError(error, {
          fallback: t(ERROR_MESSAGES.UNEXPECTED),
        })
      }
    } finally {
      setIsSubmitting(false)
    }
  }

  const onInvalid: SubmitErrorHandler<ApiKeyFormValues> = () => {
    toast.error(t('Please fix the highlighted fields before saving'))
  }

  const handleSetExpiry = (months: number, days: number, hours: number) => {
    if (months === 0 && days === 0 && hours === 0) {
      form.setValue('expired_time', undefined)
      return
    }

    const now = new Date()
    now.setMonth(now.getMonth() + months)
    now.setDate(now.getDate() + days)
    now.setHours(now.getHours() + hours)

    form.setValue('expired_time', now)
  }

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const quotaLabel = t('Quota ({{currency}})', { currency: currencyLabel })
  const quotaPlaceholder = tokensOnly
    ? t('Enter quota in tokens')
    : t('Enter quota in {{currency}}', { currency: currencyLabel })
  const routingMode = form.watch('routing_mode')
  const routingRoute = form.watch('routing_route')
  const manualGroups = form.watch('manual_groups')
  const crossGroupRetry = form.watch('cross_group_retry')
  const unlimitedQuota = form.watch('unlimited_quota')
  const routingRouteError = form.formState.errors.routing_route?.message

  return (
    <Sheet
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) {
          setEditingLegacyRouting(false)
          setPreservedSmartRoute(undefined)
          setPreservedManualGroups([])
          form.reset()
        }
      }}
    >
      <SheetContent
        className={sideDrawerContentClassName('max-w-none sm:!max-w-[620px]')}
      >
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isUpdate ? t('Update API Key') : t('Create API Key')}
          </SheetTitle>
          <SheetDescription>
            {isUpdate
              ? t('Update the API key by providing necessary info.')
              : t('Add a new API key by providing necessary info.')}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='api-key-form'
            onSubmit={form.handleSubmit(onSubmit, onInvalid)}
            className={sideDrawerFormClassName('gap-5')}
          >
            <SideDrawerSection>
              <SideDrawerSectionHeader
                title={t('Basic Information')}
                description={t('Set API key basic information')}
                icon={<KeyRound className='size-4' />}
              />
              <FormField
                control={form.control}
                name='name'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Name')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder={t('Enter a name')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='manual_groups'
                render={() => (
                  <FormItem>
                    <FormControl>
                      <ApiKeyRoutingEditor
                        mode={routingMode}
                        route={routingRoute}
                        manualGroups={manualGroups}
                        retryOnFailure={!!crossGroupRetry}
                        autoRouteOptions={autoRouteOptions}
                        realGroupOptions={realGroups}
                        defaultManualGroups={defaultManualGroups}
                        preserveUnavailableRouting={
                          Boolean(preservedSmartRoute) ||
                          preservedManualGroups.length > 0
                        }
                        routesLoading={groupsLoading}
                        disabled={isSubmitting || groupsLoading}
                        onModeChange={(mode) =>
                          form.setValue('routing_mode', mode, {
                            shouldDirty: true,
                            shouldValidate: true,
                          })
                        }
                        onRouteChange={(route) => {
                          form.setValue('routing_route', route, {
                            shouldDirty: true,
                            shouldValidate: true,
                          })
                          if (!form.getFieldState('manual_groups').isDirty) {
                            const routeGroups =
                              autoRouteOptions.find(
                                (option) => option.value === route
                              )?.groups || []
                            form.setValue(
                              'manual_groups',
                              routeGroups.slice(0, MAX_MANUAL_ROUTING_GROUPS),
                              { shouldValidate: true }
                            )
                          }
                        }}
                        onManualGroupsChange={(groups) =>
                          form.setValue('manual_groups', groups, {
                            shouldDirty: true,
                            shouldValidate: true,
                          })
                        }
                        onRetryOnFailureChange={(enabled) =>
                          form.setValue('cross_group_retry', enabled, {
                            shouldDirty: true,
                          })
                        }
                      />
                    </FormControl>
                    <FormMessage />
                    {routingRouteError && (
                      <p className='text-destructive text-sm' role='alert'>
                        {String(routingRouteError)}
                      </p>
                    )}
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='expired_time'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Expiration Time')}</FormLabel>
                    <div className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'>
                      <FormControl>
                        <DateTimePicker
                          value={field.value}
                          onChange={field.onChange}
                          placeholder={t('Never expires')}
                          className='min-w-0 [&_input[type=time]]:w-24 sm:[&_input[type=time]]:w-32'
                        />
                      </FormControl>
                      <div className='grid grid-cols-4 gap-2 sm:flex'>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          className='px-2 text-xs sm:px-3 sm:text-sm'
                          onClick={() => handleSetExpiry(0, 0, 0)}
                        >
                          {t('Never')}
                        </Button>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          className='px-2 text-xs sm:px-3 sm:text-sm'
                          onClick={() => handleSetExpiry(1, 0, 0)}
                        >
                          {t('1 Month')}
                        </Button>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          className='px-2 text-xs sm:px-3 sm:text-sm'
                          onClick={() => handleSetExpiry(0, 1, 0)}
                        >
                          {t('1 Day')}
                        </Button>
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          className='px-2 text-xs sm:px-3 sm:text-sm'
                          onClick={() => handleSetExpiry(0, 0, 1)}
                        >
                          {t('1 Hour')}
                        </Button>
                      </div>
                    </div>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {!isUpdate && (
                <FormField
                  control={form.control}
                  name='tokenCount'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Quantity')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          min='1'
                          placeholder={t('Number of keys to create')}
                          onChange={(e) =>
                            field.onChange(parseInt(e.target.value, 10) || 1)
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Create multiple API keys at once (random suffix will be added to names)'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
            </SideDrawerSection>

            <SideDrawerSection>
              <SideDrawerSectionHeader
                title={t('Quota Settings')}
                description={t('Set quota amount and limits')}
                icon={<WalletCards className='size-4' />}
              />
              {!unlimitedQuota && (
                <FormField
                  control={form.control}
                  name='remain_quota_dollars'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{quotaLabel}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          step={tokensOnly ? 1 : 0.01}
                          placeholder={quotaPlaceholder}
                          onChange={(e) =>
                            field.onChange(parseFloat(e.target.value) || 0)
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        {tokensOnly
                          ? t('Enter the quota amount in tokens')
                          : t('Enter the quota amount in {{currency}}', {
                              currency: currencyLabel,
                            })}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}

              <FormField
                control={form.control}
                name='unlimited_quota'
                render={({ field }) => (
                  <FormItem className={sideDrawerSwitchItemClassName()}>
                    <div className='flex flex-col gap-0.5'>
                      <FormLabel className='text-sm'>
                        {t('Unlimited Quota')}
                      </FormLabel>
                      <FormDescription className='text-xs'>
                        {t('Enable unlimited quota for this API key')}
                      </FormDescription>
                    </div>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </SideDrawerSection>

            <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
              <SideDrawerSection>
                <CollapsibleTrigger
                  render={
                    <button
                      type='button'
                      className='hover:bg-muted/40 flex w-full items-center gap-3 rounded-md py-1.5 text-left transition-colors'
                    />
                  }
                >
                  <SideDrawerSectionHeader
                    className='flex-1'
                    title={t('Advanced Settings')}
                    description={t('Set API key access restrictions')}
                    icon={<Settings2 className='size-4' />}
                  />
                  <ChevronDown
                    className={cn(
                      'text-muted-foreground size-4 shrink-0 transition-transform',
                      advancedOpen && 'rotate-180'
                    )}
                  />
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div className='flex flex-col gap-4 pt-2'>
                    <FormField
                      control={form.control}
                      name='model_limits'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Model Limits')}</FormLabel>
                          <FormControl>
                            <MultiSelect
                              options={models.map((m) => ({
                                label: m,
                                value: m,
                              }))}
                              selected={field.value}
                              onChange={field.onChange}
                              placeholder={t(
                                'Select models (empty for allow all)'
                              )}
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Limit which models can be used with this key')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='allow_ips'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>
                            {t('IP Whitelist (supports CIDR)')}
                          </FormLabel>
                          <FormControl>
                            <Textarea
                              {...field}
                              className='min-h-20 resize-none'
                              placeholder={t(
                                'One IP per line (empty for no restriction)'
                              )}
                              rows={3}
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Do not over-trust this feature. IP may be spoofed. Please use with nginx, CDN and other gateways.'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>
                </CollapsibleContent>
              </SideDrawerSection>
            </Collapsible>
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose
            render={<Button variant='outline' className='w-full sm:w-auto' />}
          >
            {t('Close')}
          </SheetClose>
          <Button
            type='button'
            onClick={form.handleSubmit(onSubmit, onInvalid)}
            disabled={isSubmitting || groupsLoading}
            className='w-full sm:w-auto'
          >
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
