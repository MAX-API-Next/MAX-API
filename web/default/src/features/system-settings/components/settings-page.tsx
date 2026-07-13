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
import { useMemo, useState, type ReactNode } from 'react'
import { useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { useSystemOptions, getOptionValue } from '../hooks/use-system-options'
import type { SystemOption } from '../types'
import { SettingsPageProvider } from './settings-page-context'

type SettingsPageProps<
  TSettings extends Record<string, string | number | boolean | unknown[]>,
  TSectionId extends string,
  TExtraArgs extends unknown[] = [],
> = {
  routePath: string
  defaultSettings: TSettings
  defaultSection: TSectionId
  getSectionContent: (
    sectionId: TSectionId,
    settings: TSettings,
    ...extraArgs: TExtraArgs
  ) => ReactNode
  getSectionMeta: (sectionId: TSectionId) => {
    titleKey: string
  }
  extraArgs?: TExtraArgs
  loadingMessage?: string
  resolveSettings?: (
    settings: TSettings,
    raw: SystemOption[] | undefined
  ) => TSettings
}

type SettingsPageFrameProps = {
  title: ReactNode
  children: ReactNode
}

function SettingsPageFrame(props: SettingsPageFrameProps) {
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)
  const [titleStatusContainer, setTitleStatusContainer] =
    useState<HTMLSpanElement | null>(null)

  return (
    <SettingsPageProvider
      actionsContainer={actionsContainer}
      titleStatusContainer={titleStatusContainer}
    >
      <SectionPageLayout>
        <SectionPageLayout.Title>
          <span className='inline-flex max-w-full min-w-0 items-center gap-2 align-middle'>
            <span className='truncate'>{props.title}</span>
            <span
              ref={setTitleStatusContainer}
              className='inline-flex shrink-0'
            />
          </span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <div
            ref={setActionsContainer}
            className='flex flex-wrap items-center justify-end gap-2'
          />
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex w-full flex-col gap-4'>{props.children}</div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    </SettingsPageProvider>
  )
}

function SettingsPageSkeleton() {
  return (
    <div className='flex flex-col gap-4' aria-hidden='true'>
      {Array.from({ length: 2 }, (_, sectionIndex) => (
        <div key={sectionIndex} className='overflow-hidden rounded-xl border'>
          <div className='flex flex-col gap-2 border-b px-4 py-3 sm:px-5'>
            <Skeleton className='h-5 w-40' />
            <Skeleton className='h-3.5 w-full max-w-md' />
          </div>
          <div className='grid gap-4 p-4 sm:grid-cols-2 sm:p-5'>
            {Array.from({ length: 4 }, (_, fieldIndex) => (
              <div key={fieldIndex} className='flex min-w-0 flex-col gap-2'>
                <Skeleton className='h-3.5 w-24' />
                <Skeleton className='h-8 w-full' />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

/**
 * Generic settings page component
 * Handles loading state, data fetching, and section rendering
 */
export function SettingsPage<
  TSettings extends Record<string, string | number | boolean | unknown[]>,
  TSectionId extends string,
  TExtraArgs extends unknown[] = [],
>({
  routePath,
  defaultSettings,
  defaultSection,
  getSectionContent,
  getSectionMeta,
  extraArgs,
  loadingMessage = 'Loading settings...',
  resolveSettings,
}: SettingsPageProps<TSettings, TSectionId, TExtraArgs>) {
  const { t } = useTranslation()
  const { data, error, isError, isLoading, refetch } = useSystemOptions()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const params = useParams({ from: routePath as any })
  const activeSection = (params?.section ?? defaultSection) as TSectionId
  const sectionMeta = getSectionMeta(activeSection)

  const settings = useMemo(() => {
    const baseSettings = getOptionValue(
      data?.data,
      defaultSettings
    ) as TSettings
    return resolveSettings
      ? resolveSettings(baseSettings, data?.data)
      : baseSettings
  }, [data?.data, defaultSettings, resolveSettings])

  if (isLoading) {
    return (
      <SettingsPageFrame title={t(sectionMeta.titleKey)}>
        <span className='sr-only'>{t(loadingMessage)}</span>
        <SettingsPageSkeleton />
      </SettingsPageFrame>
    )
  }

  if (isError) {
    return (
      <SettingsPageFrame title={t(sectionMeta.titleKey)}>
        <ErrorState
          description={error instanceof Error ? error.message : undefined}
          onRetry={() => void refetch()}
        />
      </SettingsPageFrame>
    )
  }

  const sectionContent = getSectionContent(
    activeSection,
    settings,
    ...((extraArgs ?? []) as TExtraArgs)
  )

  return (
    <SettingsPageFrame title={t(sectionMeta.titleKey)}>
      {sectionContent}
    </SettingsPageFrame>
  )
}
