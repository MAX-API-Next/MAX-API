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
import { Ellipsis, Languages, Palette, SunMoon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { IconGithub } from '@/assets/brand-icons'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { ConfigDrawer } from '@/components/config-drawer'
import { LanguageMenuGroup } from '@/components/language-switcher'
import { ThemeMenuGroup } from '@/components/theme-switch'

type HeaderToolsMenuProps = {
  showConfigDrawer: boolean
}

export function HeaderToolsMenu({ showConfigDrawer }: HeaderToolsMenuProps) {
  const { t } = useTranslation()
  const [configOpen, setConfigOpen] = useState(false)

  return (
    <>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          render={
            <Button
              size='icon-sm'
              variant='ghost'
              aria-label={t('Open menu')}
            />
          }
        >
          <Ellipsis aria-hidden='true' />
          <span className='sr-only'>{t('Open menu')}</span>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end'>
          <DropdownMenuGroup>
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <Languages aria-hidden='true' />
                {t('Change language')}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent side='left'>
                <LanguageMenuGroup />
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            {showConfigDrawer ? (
              <DropdownMenuItem onClick={() => setConfigOpen(true)}>
                <Palette aria-hidden='true' />
                {t('Theme Settings')}
              </DropdownMenuItem>
            ) : null}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>

      {showConfigDrawer ? (
        <ConfigDrawer
          open={configOpen}
          onOpenChange={setConfigOpen}
          showTrigger={false}
        />
      ) : null}
    </>
  )
}

type PublicHeaderToolsMenuProps = {
  githubUrl: string
  showLanguageSwitcher: boolean
  showThemeSwitch: boolean
}

export function PublicHeaderToolsMenu({
  githubUrl,
  showLanguageSwitcher,
  showThemeSwitch,
}: PublicHeaderToolsMenuProps) {
  const { t } = useTranslation()

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={
          <Button
            size='icon'
            variant='ghost'
            className='size-9'
            aria-label={t('Open menu')}
          />
        }
      >
        <Ellipsis aria-hidden='true' />
        <span className='sr-only'>{t('Open menu')}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        <DropdownMenuGroup>
          <DropdownMenuItem
            render={
              <a href={githubUrl} target='_blank' rel='noopener noreferrer' />
            }
          >
            <IconGithub aria-hidden='true' />
            GitHub
          </DropdownMenuItem>
          {showLanguageSwitcher ? (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <Languages aria-hidden='true' />
                {t('Change language')}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent side='left'>
                <LanguageMenuGroup />
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          ) : null}
          {showThemeSwitch ? (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <SunMoon aria-hidden='true' />
                {t('Theme')}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent side='left'>
                <ThemeMenuGroup />
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          ) : null}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
