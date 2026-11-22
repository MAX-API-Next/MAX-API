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
import { ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
} from '@/components/ui/dropdown-menu'
import { keyedTopNavItems } from '../config/top-nav.config'
import type { TopNavGroup, TopNavLink } from '../types'

export function TopNavMenuLink({ link }: { link: TopNavLink }) {
  return (
    <DropdownMenuItem
      disabled={link.disabled}
      className={cn(
        '[overflow-wrap:anywhere] break-words',
        !link.isActive && 'text-muted-foreground'
      )}
      render={
        link.external ? (
          <a href={link.href} target='_blank' rel='noopener noreferrer' />
        ) : (
          <Link to={link.href} disabled={link.disabled} />
        )
      }
    >
      {link.title}
    </DropdownMenuItem>
  )
}

export function TopNavGroupMenu(props: {
  group: TopNavGroup
  nested?: boolean
  className?: string
}) {
  const items = keyedTopNavItems(props.group.children).map(({ link, key }) => (
    <TopNavMenuLink key={key} link={link} />
  ))
  const contentClass = 'w-max max-w-[min(20rem,var(--available-width))]'
  if (props.nested) {
    return (
      <DropdownMenuSub>
        <DropdownMenuSubTrigger className='[overflow-wrap:anywhere]'>
          {props.group.title}
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent className={contentClass}>
          {items}
        </DropdownMenuSubContent>
      </DropdownMenuSub>
    )
  }
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        className={cn('inline-flex items-center gap-1', props.className)}
      >
        <span className='max-w-48 truncate'>{props.group.title}</span>
        <ChevronDown className='size-3.5 shrink-0' aria-hidden='true' />
      </DropdownMenuTrigger>
      <DropdownMenuContent className={contentClass}>
        {items}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
