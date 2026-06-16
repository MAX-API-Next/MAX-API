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
import {
  ArrowRight,
  BookOpen,
  CircleDollarSign,
  FlaskConical,
  RadioTower,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import { HeroTerminalDemo } from '../hero-terminal-demo'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

export function Hero(props: HeroProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const docsUrl =
    (status?.docs_link as string | undefined) ||
    'https://github.com/MAX-API-Next/MAX-API'

  const docsButton = docsUrl.startsWith('http') ? (
    <Button
      variant='outline'
      className='border-foreground/20 hover:bg-foreground/[0.06] h-10 rounded-none bg-transparent px-4 text-sm'
      render={<a href={docsUrl} target='_blank' rel='noopener noreferrer' />}
    >
      <BookOpen data-icon='inline-start' />
      {t('Docs')}
    </Button>
  ) : (
    <Button
      variant='outline'
      className='border-foreground/20 hover:bg-foreground/[0.06] h-10 rounded-none bg-transparent px-4 text-sm'
      render={<Link to={docsUrl} />}
    >
      <BookOpen data-icon='inline-start' />
      {t('Docs')}
    </Button>
  )

  return (
    <section className='relative isolate overflow-hidden px-4 pt-24 pb-10 sm:px-6 md:pt-28'>
      <div
        aria-hidden='true'
        className='absolute inset-0 -z-20 bg-[linear-gradient(118deg,#f8fafc_0%,#eef6f4_42%,#f6f1e7_100%)] dark:bg-[linear-gradient(118deg,#08111a_0%,#0d1717_48%,#171309_100%)]'
      />
      <div
        aria-hidden='true'
        className='absolute inset-0 -z-10 [background-image:linear-gradient(to_right,rgba(15,23,42,0.09)_1px,transparent_1px),linear-gradient(to_bottom,rgba(15,23,42,0.07)_1px,transparent_1px)] [background-size:42px_42px] opacity-[0.46] dark:[background-image:linear-gradient(to_right,rgba(203,213,225,0.24)_1px,transparent_1px),linear-gradient(to_bottom,rgba(203,213,225,0.16)_1px,transparent_1px)] dark:opacity-[0.18]'
      />
      <div
        aria-hidden='true'
        className='editorial-grain absolute inset-0 -z-10'
      />
      <div
        aria-hidden='true'
        className='from-background via-background/78 absolute inset-x-0 bottom-0 -z-10 h-48 bg-linear-to-t to-transparent'
      />

      <div className='mx-auto flex min-h-[78svh] max-w-7xl flex-col justify-between gap-10'>
        {/* Masthead nameplate */}
        <div
          className='landing-animate-fade-up border-foreground/15 flex flex-wrap items-end justify-between gap-3 border-b pb-4 opacity-0'
          style={{ animationDelay: '0ms' }}
        >
          <div className='flex items-baseline gap-3'>
            <span className='editorial-numeral text-foreground text-2xl font-black md:text-3xl'>
              №01
            </span>
            <span className='text-muted-foreground text-[11px] font-semibold tracking-[0.32em] uppercase'>
              {t('AI model and Agent governance')}
            </span>
          </div>
          <div className='text-muted-foreground inline-flex items-center gap-2 text-[11px] font-medium tracking-[0.14em] uppercase'>
            <FlaskConical className='size-3.5 text-emerald-600 dark:text-emerald-300' />
            {t('Research-driven AI infrastructure project')}
          </div>
        </div>

        <div className='grid items-end gap-10 lg:grid-cols-[minmax(0,0.86fr)_minmax(0,1.14fr)]'>
          <div className='max-w-3xl'>
            <h1
              className='landing-animate-fade-up text-foreground font-serif text-[clamp(3rem,8vw,7rem)] leading-[0.9] font-black tracking-[-0.02em] opacity-0'
              style={{ animationDelay: '70ms' }}
            >
              MAX API
            </h1>
            <p
              className='landing-animate-fade-up text-foreground/85 mt-4 max-w-2xl font-serif text-[clamp(1.25rem,2.4vw,1.9rem)] leading-[1.18] font-medium opacity-0'
              style={{ animationDelay: '120ms' }}
            >
              {t('The governance layer for AI models and Agents.')}
            </p>

            <p
              className='landing-animate-fade-up text-muted-foreground mt-6 max-w-2xl text-base leading-8 opacity-0 md:text-[1.05rem]'
              style={{ animationDelay: '180ms' }}
            >
              {t(
                'Unify access, routing, billing, observability, and audit for AI models and Agent workloads in a single self-hosted service layer.'
              )}
            </p>

            <div
              className='landing-animate-fade-up mt-7 flex flex-wrap items-center gap-3 opacity-0'
              style={{ animationDelay: '240ms' }}
            >
              {props.isAuthenticated ? (
                <Button
                  className='h-10 rounded-none px-5 text-sm'
                  render={<Link to='/dashboard' />}
                >
                  {t('Go to Dashboard')}
                  <ArrowRight data-icon='inline-end' />
                </Button>
              ) : (
                <>
                  <Button
                    className='h-10 rounded-none px-5 text-sm'
                    render={<Link to='/sign-up' />}
                  >
                    {t('Get Started')}
                    <ArrowRight data-icon='inline-end' />
                  </Button>
                  <Button
                    variant='outline'
                    className='border-foreground/20 hover:bg-foreground/[0.06] h-10 rounded-none bg-transparent px-4 text-sm'
                    render={<Link to='/pricing' />}
                  >
                    <CircleDollarSign data-icon='inline-start' />
                    {t('View Pricing')}
                  </Button>
                </>
              )}
              {docsButton}
            </div>

            {/* Quickstart specimen — signature motif */}
            <figure
              className='landing-animate-fade-up border-foreground/15 bg-card/55 mt-8 max-w-xl overflow-hidden rounded-lg border opacity-0 backdrop-blur'
              style={{ animationDelay: '300ms' }}
            >
              <figcaption className='border-foreground/12 text-muted-foreground flex items-center justify-between border-b px-4 py-2 text-[10px] font-semibold tracking-[0.22em] uppercase'>
                <span>{t('Quickstart')}</span>
                <span>{t('One endpoint, any model')}</span>
              </figcaption>
              <code className='text-foreground block overflow-x-auto px-4 py-3 font-mono text-[13px] leading-6'>
                <span className='text-muted-foreground block'>
                  # OpenAI-compatible endpoint
                </span>
                <span className='block'>
                  <span className='text-emerald-600 dark:text-emerald-300'>
                    POST
                  </span>{' '}
                  /v1/chat/completions
                </span>
                <span className='block'>
                  <span className='text-muted-foreground'>model</span>{' '}
                  <span className='text-amber-600 dark:text-amber-300'>
                    "gpt-5 · claude · gemini · deepseek"
                  </span>
                </span>
              </code>
            </figure>
          </div>

          <div
            className='landing-animate-fade-up opacity-0'
            style={{ animationDelay: '340ms' }}
          >
            <HeroTerminalDemo />
          </div>
        </div>

        {/* Signals ledger */}
        <div
          className='landing-animate-fade-up border-foreground/12 bg-foreground/10 grid gap-px overflow-hidden rounded-xl border opacity-0 sm:grid-cols-3'
          style={{ animationDelay: '420ms' }}
        >
          <HeroSignal
            index='01'
            icon={RadioTower}
            title={t('AI asset governance')}
            description={t(
              'Model catalogs, provider channels, mappings, permissions, pricing, and task protocols.'
            )}
          />
          <HeroSignal
            index='02'
            icon={CircleDollarSign}
            title={t('Intuitive cost audit')}
            description={t(
              'Readable model pricing, quota flow, task rate-cards, refunds, and usage attribution.'
            )}
          />
          <HeroSignal
            index='03'
            icon={ShieldCheck}
            title={t('Boundary control')}
            description={t(
              'Agent token scopes, model access, audit visibility, retention, and admin controls.'
            )}
          />
        </div>
      </div>
    </section>
  )
}

function HeroSignal(props: {
  icon: LucideIcon
  index: string
  title: string
  description: string
}) {
  const Icon = props.icon

  return (
    <div className='group bg-background/70 hover:bg-background relative p-5 backdrop-blur transition-colors'>
      <div className='mb-3 flex items-center justify-between'>
        <div className='border-border bg-card flex size-9 items-center justify-center rounded-lg border'>
          <Icon className='size-4 text-emerald-700 dark:text-emerald-300' />
        </div>
        <span className='editorial-numeral text-muted-foreground/70 text-sm font-bold'>
          {props.index}
        </span>
      </div>
      <h2 className='text-foreground text-sm font-semibold'>{props.title}</h2>
      <p className='text-muted-foreground mt-1.5 text-xs leading-5'>
        {props.description}
      </p>
      <span className='absolute inset-x-0 bottom-0 h-px scale-x-0 bg-emerald-500/60 transition-transform duration-300 group-hover:scale-x-100' />
    </div>
  )
}
