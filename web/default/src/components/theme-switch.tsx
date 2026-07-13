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
import { useEffect } from 'react'
import { Check, Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useTheme } from '@/context/theme-provider'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

export function ThemeMenuGroup() {
  const { t } = useTranslation()
  const { theme, setTheme } = useTheme()

  return (
    <DropdownMenuGroup>
      <DropdownMenuItem onClick={() => setTheme('light')}>
        {t('Light')}
        <Check
          className={cn('ms-auto', theme !== 'light' && 'hidden')}
          aria-hidden='true'
        />
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => setTheme('dark')}>
        {t('Dark')}
        <Check
          className={cn('ms-auto', theme !== 'dark' && 'hidden')}
          aria-hidden='true'
        />
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => setTheme('system')}>
        {t('System')}
        <Check
          className={cn('ms-auto', theme !== 'system' && 'hidden')}
          aria-hidden='true'
        />
      </DropdownMenuItem>
    </DropdownMenuGroup>
  )
}

export function ThemeSwitch() {
  const { t } = useTranslation()
  const { theme } = useTheme()

  /* Update theme-color meta tag
   * when theme is updated */
  useEffect(() => {
    const themeColor = theme === 'dark' ? '#020817' : '#fff'
    const metaThemeColor = document.querySelector("meta[name='theme-color']")
    if (metaThemeColor) metaThemeColor.setAttribute('content', themeColor)
  }, [theme])

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={<Button variant='ghost' size='icon' className='h-9 w-9' />}
      >
        <Sun className='size-[1.2rem] scale-100 rotate-0 transition-all dark:scale-0 dark:-rotate-90' />
        <Moon className='absolute size-[1.2rem] scale-0 rotate-90 transition-all dark:scale-100 dark:rotate-0' />
        <span className='sr-only'>{t('Toggle theme')}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        <ThemeMenuGroup />
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
