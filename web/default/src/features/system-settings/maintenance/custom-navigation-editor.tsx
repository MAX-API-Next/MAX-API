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
import { useFieldArray, type Control } from 'react-hook-form'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { SettingsControlChildren } from '../components/settings-form-layout'
import type { HeaderNavFormValues } from './header-navigation-schema'

type EditorControl = Control<HeaderNavFormValues>
type NavItemPath = `customItems.${number}`
type NavLinkPath = NavItemPath | `${NavItemPath}.children.${number}`

function LinkFields(props: {
  control: EditorControl
  path: NavLinkPath
  hrefOptional?: boolean
  onRemove: () => void
  removeLabel: string
}) {
  const { t } = useTranslation()
  return (
    <div className='grid items-start gap-2 sm:grid-cols-[1fr_1fr_auto]'>
      <FormField
        control={props.control}
        name={`${props.path}.title`}
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Navigation name')}</FormLabel>
            <FormControl>
              <Input {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={props.control}
        name={`${props.path}.href`}
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {props.hrefOptional
                ? t('Navigation link (optional)')
                : t('Navigation link')}
            </FormLabel>
            <FormControl>
              <Input placeholder='/path' {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <Button
        type='button'
        variant='ghost'
        size='sm'
        className='text-muted-foreground justify-self-start sm:mt-5'
        onClick={props.onRemove}
        aria-label={props.removeLabel}
        title={props.removeLabel}
      >
        <Trash2 aria-hidden='true' />
        <span className='sm:hidden'>{props.removeLabel}</span>
      </Button>
    </div>
  )
}

function NavigationItemEditor(props: {
  control: EditorControl
  index: number
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const path = `customItems.${props.index}` as const
  const children = useFieldArray({
    control: props.control,
    name: `${path}.children`,
  })
  return (
    <div className='space-y-3 py-3'>
      <LinkFields
        control={props.control}
        path={path}
        hrefOptional={children.fields.length > 0}
        onRemove={props.onRemove}
        removeLabel={t('Remove navigation item')}
      />
      <SettingsControlChildren className='space-y-3'>
        {children.fields.map((field, index) => (
          <LinkFields
            key={field.id}
            control={props.control}
            path={`${path}.children.${index}`}
            onRemove={() => children.remove(index)}
            removeLabel={t('Remove sublink')}
          />
        ))}
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={() => children.append({ title: '', href: '' })}
        >
          <Plus aria-hidden='true' />
          {t('Add sublink')}
        </Button>
      </SettingsControlChildren>
    </div>
  )
}

export function CustomNavigationEditor(props: { control: EditorControl }) {
  const { t } = useTranslation()
  const items = useFieldArray({ control: props.control, name: 'customItems' })
  return (
    <div>
      <div className='divide-y'>
        {items.fields.map((field, index) => (
          <NavigationItemEditor
            key={`${field.id}-${index}`}
            control={props.control}
            index={index}
            onRemove={() => items.remove(index)}
          />
        ))}
      </div>
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={() => items.append({ title: '', href: '', children: [] })}
      >
        <Plus aria-hidden='true' />
        {t('Add navigation item')}
      </Button>
    </div>
  )
}
