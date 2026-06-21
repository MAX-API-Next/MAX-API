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
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Skeleton } from '@/components/ui/skeleton'

type AuthLayoutProps = {
  children: React.ReactNode
  variant?: 'center' | 'split'
}

export function AuthLayout({ children, variant = 'center' }: AuthLayoutProps) {
  const { t } = useTranslation()
  const { systemName, logo, loading } = useSystemConfig()

  return (
    <div className='auth-shell relative min-h-svh overflow-hidden'>
      <div className='auth-shell-bg' aria-hidden='true'>
        <div className='auth-shell-grid' />
        <div className='auth-shell-orbit auth-shell-orbit-one' />
        <div className='auth-shell-orbit auth-shell-orbit-two' />
        <div className='auth-shell-orbit auth-shell-orbit-three' />
      </div>

      <Link
        to='/'
        className={cn(
          'auth-brand-link absolute z-20 flex items-center gap-2 transition-opacity hover:opacity-80',
          'top-4 left-4 sm:top-6 sm:left-6'
        )}
      >
        <div className='auth-brand-logo'>
          {loading ? (
            <Skeleton className='absolute inset-0 rounded-full' />
          ) : (
            <img
              src={logo}
              alt={t('Logo')}
              className='size-full object-cover'
            />
          )}
        </div>
        {loading ? (
          <Skeleton className='h-6 w-24 rounded-full' />
        ) : (
          <h1 className='text-lg font-medium tracking-tight sm:text-xl'>
            {systemName}
          </h1>
        )}
      </Link>

      <div className='relative z-10 container flex min-h-svh items-stretch px-4 py-6 sm:px-6 lg:px-8'>
        {variant === 'split' ? (
          <div className='auth-layout-split flex w-full items-center'>
            {children}
          </div>
        ) : (
          <div className='auth-layout-center mx-auto flex w-full max-w-[32rem] flex-col justify-center py-8 sm:py-10'>
            {children}
          </div>
        )}
      </div>
    </div>
  )
}
