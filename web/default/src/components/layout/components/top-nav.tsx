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
import { Link } from '@tanstack/react-router'
import { Menu } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { TopNavItem } from '../types'
import { TopNavGroupMenu, TopNavMenuLink } from './top-nav-group'

type TopNavProps = React.HTMLAttributes<HTMLElement> & {
  links: TopNavItem[]
}

/**
 * 顶部导航栏组件
 * 在大屏幕显示水平导航，在小屏幕显示下拉菜单
 */
export function TopNav({ className, links, ...props }: TopNavProps) {
  const { t } = useTranslation()

  return (
    <>
      {/* 移动端下拉菜单 */}
      <div className='lg:hidden'>
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            render={
              <Button
                size='icon-sm'
                variant='outline'
                aria-label={t('Toggle navigation menu')}
              />
            }
          >
            <Menu />
            <span className='sr-only'>{t('Toggle navigation menu')}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            side='bottom'
            align='start'
            className='w-max max-w-[min(20rem,var(--available-width))]'
          >
            <DropdownMenuGroup>
              {links.map((link, index) =>
                'children' in link ? (
                  <TopNavGroupMenu key={index} group={link} nested />
                ) : (
                  <TopNavMenuLink key={index} link={link} />
                )
              )}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* 桌面端水平导航 */}
      <nav
        className={cn(
          'hidden items-center gap-4 overflow-x-auto whitespace-nowrap lg:flex xl:gap-6',
          className
        )}
        {...props}
      >
        {links.map((link, index) => {
          const linkClass =
            'text-muted-foreground hover:text-primary shrink-0 text-sm font-medium transition-colors'
          if ('children' in link)
            return (
              <TopNavGroupMenu key={index} group={link} className={linkClass} />
            )
          const className = cn(
            linkClass,
            'max-w-48 truncate',
            link.isActive && 'text-foreground'
          )
          return link.external ? (
            <a
              key={index}
              href={link.href}
              target='_blank'
              rel='noopener noreferrer'
              className={className}
            >
              {link.title}
            </a>
          ) : (
            <Link
              key={index}
              to={link.href}
              disabled={link.disabled}
              className={className}
            >
              {link.title}
            </Link>
          )
        })}
      </nav>
    </>
  )
}
