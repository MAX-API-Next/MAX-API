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
import { ArrowRight, ServerCog } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { AnimateInView } from '@/components/animate-in-view'

interface CTAProps {
  className?: string
  isAuthenticated?: boolean
}

export function CTA(props: CTAProps) {
  const { t } = useTranslation()

  if (props.isAuthenticated) {
    return null
  }

  return (
    <section className='bg-background relative z-10 px-4 py-20 sm:px-6 md:py-28'>
      <AnimateInView
        className='relative mx-auto max-w-7xl overflow-hidden rounded-xl border border-white/12 bg-[#0b1118] text-slate-100 shadow-[0_22px_70px_-44px_rgba(0,0,0,0.86)]'
        animation='scale-in'
      >
        <div
          aria-hidden='true'
          className='absolute inset-0 [background-image:linear-gradient(to_right,rgba(203,213,225,0.35)_1px,transparent_1px),linear-gradient(to_bottom,rgba(203,213,225,0.25)_1px,transparent_1px)] [background-size:36px_36px] opacity-[0.16]'
        />
        <div className='editorial-grain absolute inset-0' aria-hidden='true' />
        <span
          aria-hidden='true'
          className='absolute inset-x-0 top-0 h-px bg-linear-to-r from-transparent via-emerald-300/70 to-transparent'
        />

        {/* Colophon nameplate */}
        <div className='relative flex items-center justify-between gap-3 border-b border-white/10 px-6 py-3 md:px-8'>
          <div className='inline-flex items-center gap-2 text-[11px] font-semibold tracking-[0.26em] text-emerald-200/90 uppercase'>
            <ServerCog className='size-3.5' />
            {t('Deployable AI service infrastructure')}
          </div>
          <span className='editorial-numeral text-sm font-bold text-slate-500'>
            §
          </span>
        </div>

        <div className='relative grid gap-8 p-6 md:p-10 lg:grid-cols-[1.4fr_auto] lg:items-end'>
          <div className='max-w-3xl'>
            <h2 className='font-serif text-4xl leading-[1.02] font-black tracking-[-0.02em] md:text-[3.5rem]'>
              {t('Build the governance layer for AI models and Agents')}
            </h2>
            <p className='mt-5 max-w-2xl text-sm leading-7 text-slate-400 md:text-base'>
              {t(
                'Start with a self-hosted gateway, then keep improving model routing, pricing rules, audit boundaries, and Agent workload visibility as AI usage grows.'
              )}
            </p>
          </div>
          <div className='flex flex-wrap gap-3'>
            <Button
              className='h-11 rounded-none bg-slate-100 px-5 text-sm text-slate-950 hover:bg-white'
              render={<Link to='/sign-up' />}
            >
              {t('Get Started')}
              <ArrowRight data-icon='inline-end' />
            </Button>
            <Button
              variant='outline'
              className='h-11 rounded-none border-white/20 bg-transparent px-5 text-sm text-slate-100 hover:bg-white/10 hover:text-white'
              render={<Link to='/pricing' />}
            >
              {t('View Pricing')}
            </Button>
          </div>
        </div>
      </AnimateInView>
    </section>
  )
}
