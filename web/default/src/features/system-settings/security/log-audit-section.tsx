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
import { useState } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { Switch } from '@/components/ui/switch'
import { updateSystemOption } from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { safeNumberFieldProps } from '../utils/numeric-field'

const logAuditSchema = z.object({
  LogRequestContentEnabled: z.boolean(),
  LogResponseContentEnabled: z.boolean(),
  LogContentMaxCharacters: z.number().int().min(1).max(1000000),
})

type LogAuditFormValues = z.infer<typeof logAuditSchema>

type LogAuditSectionProps = {
  defaultValues: LogAuditFormValues
}

export function LogAuditSection({ defaultValues }: LogAuditSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<LogAuditFormValues>({
    resolver: zodResolver(logAuditSchema),
    defaultValues,
  })
  const auditEnabled =
    form.watch('LogRequestContentEnabled') ||
    form.watch('LogResponseContentEnabled')

  const [isSaving, setIsSaving] = useState(false)

  useResetForm(form, defaultValues)

  const onSubmit = async (values: LogAuditFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) => value !== defaultValues[key as keyof LogAuditFormValues]
    )
    if (updates.length === 0) return

    setIsSaving(true)
    let didApplyAny = false
    try {
      for (const [key, value] of updates) {
        const res = await updateSystemOption({ key, value })
        if (!res.success) {
          throw new Error(res.message || t('Failed to update setting'))
        }
        didApplyAny = true
      }

      await queryClient.invalidateQueries({ queryKey: ['system-options'] })
      toast.success(t('Setting updated successfully'))
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to update setting')
      if (didApplyAny) {
        try {
          await queryClient.invalidateQueries({ queryKey: ['system-options'] })
        } catch {
          /* empty */
        }
      }
      toast.error(message)
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <SettingsSection title={t('Log Audit')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={isSaving}
            saveLabel='Save log audit settings'
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Requires Record quota usage to be enabled in Operations > Log Maintenance.'
            )}
          </p>
          <FormField
            control={form.control}
            name='LogRequestContentEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Record request content')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Store request prompts and messages in admin-only usage log details.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </SettingsSwitchItem>
            )}
          />
          <FormField
            control={form.control}
            name='LogResponseContentEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Record response content')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Store model output text in admin-only usage log details.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </SettingsSwitchItem>
            )}
          />
          <FormField
            control={form.control}
            name='LogContentMaxCharacters'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Max logged content characters')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min='1'
                    max='1000000'
                    disabled={!auditEnabled}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Input/output content logging can store sensitive user data. Keep it disabled unless your compliance policy allows it.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
