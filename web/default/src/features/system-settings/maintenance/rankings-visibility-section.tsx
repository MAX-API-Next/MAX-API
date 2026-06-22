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
import { useMemo } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import {
  SettingsControlChildren,
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { HEADER_NAV_DEFAULT, type HeaderNavAccessConfig } from './config'
import { useRankingsModuleOption } from './use-rankings-module-option'

const rankingsVisibilitySchema = z.object({
  rankingsEnabled: z.boolean(),
  rankingsRequireAuth: z.boolean(),
})

type RankingsVisibilityFormValues = z.infer<typeof rankingsVisibilitySchema>

type RankingsVisibilitySectionProps = {
  config: HeaderNavAccessConfig
}

const toFormValues = (
  config: HeaderNavAccessConfig
): RankingsVisibilityFormValues => ({
  rankingsEnabled: Boolean(config.enabled),
  rankingsRequireAuth: Boolean(config.requireAuth),
})

export function RankingsVisibilitySection({
  config,
}: RankingsVisibilitySectionProps) {
  const { t } = useTranslation()
  const rankingsOption = useRankingsModuleOption()
  const formDefaults = useMemo(() => toFormValues(config), [config])

  const form = useForm<RankingsVisibilityFormValues>({
    resolver: zodResolver(rankingsVisibilitySchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const onSubmit = async (values: RankingsVisibilityFormValues) => {
    await rankingsOption.updateRankingsModule((current) => ({
      ...current,
      enabled: values.rankingsEnabled,
      requireAuth: values.rankingsRequireAuth,
    }))
  }

  const resetToDefault = () => {
    form.reset(toFormValues(HEADER_NAV_DEFAULT.rankings))
  }

  return (
    <SettingsSection title={t('Rankings')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            onReset={resetToDefault}
            isSaving={rankingsOption.isPending}
            resetLabel='Reset to default'
            saveLabel='Save rankings'
          />

          <SettingsControlGroup>
            <FormField
              control={form.control}
              name='rankingsEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable rankings page')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Allow users to open the rankings page and query live ranking data.'
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
              name='rankingsRequireAuth'
              render={({ field }) => (
                <SettingsControlChildren>
                  <SettingsSwitchItem className='border-b-0 py-2'>
                    <SettingsSwitchContent>
                      <FormLabel>
                        {t('Require login to view rankings')}
                      </FormLabel>
                      <FormDescription>
                        {t(
                          'Visitors must authenticate before accessing the rankings page.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={!form.watch('rankingsEnabled')}
                      />
                    </FormControl>
                    <FormMessage />
                  </SettingsSwitchItem>
                </SettingsControlChildren>
              )}
            />
          </SettingsControlGroup>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
