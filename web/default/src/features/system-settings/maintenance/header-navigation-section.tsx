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
  SettingsForm,
  SettingsSwitchContent,
  SettingsControlGroup,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { HEADER_NAV_DEFAULT, type HeaderNavModulesConfig } from './config'
import { useHeaderNavModulesOption } from './use-header-nav-modules-option'

const headerNavSchema = z.object({
  home: z.boolean(),
  console: z.boolean(),
  pricingEnabled: z.boolean(),
  pricingRequireAuth: z.boolean(),
  docs: z.boolean(),
  about: z.boolean(),
})

type HeaderNavFormValues = z.infer<typeof headerNavSchema>

type HeaderNavigationSectionProps = {
  config: HeaderNavModulesConfig
}

const toFormValues = (config: HeaderNavModulesConfig): HeaderNavFormValues => ({
  home:
    config.home === undefined ? HEADER_NAV_DEFAULT.home : Boolean(config.home),
  console:
    config.console === undefined
      ? HEADER_NAV_DEFAULT.console
      : Boolean(config.console),
  pricingEnabled:
    config.pricing?.enabled === undefined
      ? HEADER_NAV_DEFAULT.pricing.enabled
      : Boolean(config.pricing.enabled),
  pricingRequireAuth:
    config.pricing?.requireAuth === undefined
      ? HEADER_NAV_DEFAULT.pricing.requireAuth
      : Boolean(config.pricing.requireAuth),
  docs:
    config.docs === undefined ? HEADER_NAV_DEFAULT.docs : Boolean(config.docs),
  about:
    config.about === undefined
      ? HEADER_NAV_DEFAULT.about
      : Boolean(config.about),
})

export function HeaderNavigationSection({
  config,
}: HeaderNavigationSectionProps) {
  const { t } = useTranslation()
  const headerNavOption = useHeaderNavModulesOption()
  const formDefaults = useMemo(() => toFormValues(config), [config])

  const form = useForm<HeaderNavFormValues>({
    resolver: zodResolver(headerNavSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const onSubmit = async (values: HeaderNavFormValues) => {
    await headerNavOption.updateHeaderNavModules((current) => ({
      ...current,
      home: values.home,
      console: values.console,
      docs: values.docs,
      about: values.about,
      pricing: {
        ...(current.pricing ?? HEADER_NAV_DEFAULT.pricing),
        enabled: values.pricingEnabled,
        requireAuth: values.pricingRequireAuth,
      },
    }))
  }

  const resetToDefault = () => {
    form.reset(toFormValues(HEADER_NAV_DEFAULT))
  }

  const simpleModules: Array<{
    key: keyof HeaderNavFormValues
    title: string
    description: string
  }> = [
    {
      key: 'home',
      title: t('Home'),
      description: t('Landing page with system overview.'),
    },
    {
      key: 'console',
      title: t('Console'),
      description: t('User dashboard and quota controls.'),
    },
    {
      key: 'docs',
      title: t('Docs'),
      description: t('Documentation or external knowledge base.'),
    },
    {
      key: 'about',
      title: t('About'),
      description: t('Static page describing the platform.'),
    },
  ]

  const accessModules: Array<{
    enabledKey: keyof HeaderNavFormValues
    requireAuthKey: keyof HeaderNavFormValues
    requireAuthDependsOn: 'pricingEnabled'
    title: string
    description: string
    requireAuthTitle: string
    requireAuthDescription: string
  }> = [
    {
      enabledKey: 'pricingEnabled',
      requireAuthKey: 'pricingRequireAuth',
      requireAuthDependsOn: 'pricingEnabled',
      title: t('Model Square'),
      description: t('Public model catalog and pricing page.'),
      requireAuthTitle: t('Require login to view models'),
      requireAuthDescription: t(
        'Visitors must authenticate before accessing the pricing directory.'
      ),
    },
  ]

  return (
    <SettingsSection title={t('Header navigation')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            onReset={resetToDefault}
            isSaving={headerNavOption.isPending}
            resetLabel='Reset to default'
            saveLabel='Save navigation'
          />
          <div className='grid gap-4 md:grid-cols-2'>
            {simpleModules.map((module) => (
              <FormField
                key={module.key}
                control={form.control}
                name={module.key}
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{module.title}</FormLabel>
                      <FormDescription>{module.description}</FormDescription>
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
            ))}
          </div>

          <div className='grid gap-4 lg:grid-cols-2'>
            {accessModules.map((module) => (
              <SettingsControlGroup key={module.enabledKey}>
                <FormField
                  control={form.control}
                  name={module.enabledKey}
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>{module.title}</FormLabel>
                        <FormDescription>{module.description}</FormDescription>
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
                  name={module.requireAuthKey}
                  render={({ field }) => (
                    <SettingsControlChildren>
                      <SettingsSwitchItem className='border-b-0 py-2'>
                        <SettingsSwitchContent>
                          <FormLabel>{module.requireAuthTitle}</FormLabel>
                          <FormDescription>
                            {module.requireAuthDescription}
                          </FormDescription>
                        </SettingsSwitchContent>
                        <FormControl>
                          <Switch
                            checked={field.value}
                            onCheckedChange={field.onChange}
                            disabled={!form.watch(module.requireAuthDependsOn)}
                          />
                        </FormControl>
                        <FormMessage />
                      </SettingsSwitchItem>
                    </SettingsControlChildren>
                  )}
                />
              </SettingsControlGroup>
            ))}
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
