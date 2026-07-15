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
  KeyRound,
  LockKeyhole,
  Network,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DEFAULT_LOGO } from '@/lib/constants'
import { cn } from '@/lib/utils'
import { useStatus } from '@/hooks/use-status'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { NeuralSphere } from '@/components/neural-sphere'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

const HERO_BADGES = [
  {
    label: 'Multi-provider model access',
    icon: Network,
  },
  {
    label: 'AgentOps permission boundaries',
    icon: LockKeyhole,
  },
  {
    label: 'Auditable cost settlement',
    icon: CircleDollarSign,
  },
] as const

const HERO_SIGNALS = [
  {
    value: '50+',
    label: 'model and platform ecosystems',
  },
  {
    value: '100+',
    label: 'governed model price rules',
  },
  {
    value: '11+',
    label: 'async multimodal task adapters',
  },
] as const

const HERO_PANELS = [
  {
    title: 'Model access',
    description:
      'Unify OpenAI-compatible APIs, Responses, Claude, Gemini, Azure, Bedrock, Vertex, Ollama, domestic platforms, and multimodal endpoints behind one governed entrance.',
    icon: Network,
  },
  {
    title: 'AgentOps control',
    description:
      'Separate Agent tokens, model scope, quota, routing policy, request trace, and failure attribution before long chains become operational debt.',
    icon: KeyRound,
  },
  {
    title: 'Operational governance',
    description:
      'Keep channel health, cost audit, task state, logs, admin visibility, and private deployment boundaries explicit over time.',
    icon: ShieldCheck,
  },
] as const

export function Hero(props: HeroProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const docsUrl =
    (status?.docs_link as string | undefined) ||
    'https://github.com/MAX-API-Next/MAX-API'

  const docsButton = docsUrl.startsWith('http') ? (
    <Button
      variant='outline'
      size='lg'
      className='home-hero-button home-hero-button--ghost'
      render={<a href={docsUrl} target='_blank' rel='noopener noreferrer' />}
    >
      <BookOpen data-icon='inline-start' />
      {t('Docs')}
    </Button>
  ) : (
    <Button
      variant='outline'
      size='lg'
      className='home-hero-button home-hero-button--ghost'
      render={<Link to={docsUrl} />}
    >
      <BookOpen data-icon='inline-start' />
      {t('Docs')}
    </Button>
  )

  return (
    <section
      className={cn(
        'home-hero relative isolate overflow-hidden px-4 pt-0 pb-14 text-white sm:px-6 lg:pb-20',
        props.className
      )}
    >
      <div className='home-hero-grid' aria-hidden='true' />
      <div className='home-hero-scanline' aria-hidden='true' />
      <div className='home-hero-top-spacer' aria-hidden='true' />

      <div className='home-hero-layout relative mx-auto grid min-h-[calc(100svh-var(--home-hero-safe-space)-3rem)] max-w-7xl items-center gap-10 lg:grid-cols-[minmax(0,0.94fr)_minmax(440px,1.06fr)]'>
        <div className='flex min-w-0 flex-col gap-8'>
          <div className='flex flex-col gap-5'>
            <p
              className='landing-animate-fade-up home-eyebrow opacity-0'
              style={{ animationDelay: '70ms' }}
            >
              {t('Built by the MAX API Next community')}
            </p>
            <h1
              className='landing-animate-fade-up home-hero-title opacity-0'
              style={{ animationDelay: '120ms' }}
            >
              <span>MAX API</span>
              <strong>
                {t('Governance infrastructure for AI Models and Agents')}
              </strong>
            </h1>
            <p
              className='landing-animate-fade-up home-hero-lede opacity-0'
              style={{ animationDelay: '180ms' }}
            >
              {t(
                'When AI applications move from demos to production, the hard problem is no longer a single model call. MAX API sits between applications, Agents, users, organizations, and upstream model platforms to govern access, routing, quota, billing, logs, and audit boundaries.'
              )}
            </p>
          </div>

          <div
            className='landing-animate-fade-up flex flex-wrap gap-2 opacity-0'
            style={{ animationDelay: '230ms' }}
          >
            {HERO_BADGES.map((badge) => {
              const Icon = badge.icon
              return (
                <Badge
                  key={badge.label}
                  variant='outline'
                  className='home-capability-badge'
                >
                  <Icon data-icon='inline-start' />
                  {t(badge.label)}
                </Badge>
              )
            })}
          </div>

          <div
            className='landing-animate-fade-up flex flex-wrap items-center gap-3 opacity-0'
            style={{ animationDelay: '290ms' }}
          >
            {props.isAuthenticated ? (
              <Button
                size='lg'
                className='home-hero-button home-hero-button--primary'
                render={<Link to='/dashboard' />}
              >
                {t('Go to Dashboard')}
                <ArrowRight data-icon='inline-end' />
              </Button>
            ) : (
              <>
                <Button
                  size='lg'
                  className='home-hero-button home-hero-button--primary'
                  render={<Link to='/sign-up' />}
                >
                  {t('Get Started')}
                  <ArrowRight data-icon='inline-end' />
                </Button>
                <Button
                  variant='outline'
                  size='lg'
                  className='home-hero-button home-hero-button--ghost'
                  render={<Link to='/pricing' />}
                >
                  <CircleDollarSign data-icon='inline-start' />
                  {t('View Pricing')}
                </Button>
              </>
            )}
            {docsButton}
          </div>

          <div
            className='landing-animate-fade-up home-signal-grid opacity-0'
            style={{ animationDelay: '360ms' }}
          >
            {HERO_SIGNALS.map((signal) => (
              <div key={signal.label} className='home-signal-card'>
                <span className='home-signal-value'>{signal.value}</span>
                <span className='home-signal-label'>{t(signal.label)}</span>
              </div>
            ))}
          </div>
        </div>

        <div
          className='landing-animate-fade-up relative min-w-0 opacity-0'
          style={{ animationDelay: '260ms' }}
        >
          <NeuralSphere logo={DEFAULT_LOGO} name='MAX API' />
        </div>
      </div>

      <div className='relative mx-auto mt-4 grid max-w-7xl gap-3 md:grid-cols-3'>
        {HERO_PANELS.map((panel, index) => {
          const Icon = panel.icon
          return (
            <article
              key={panel.title}
              className='landing-animate-fade-up home-glass-panel home-hero-info-panel opacity-0'
              style={{ animationDelay: `${420 + index * 70}ms` }}
            >
              <div className='home-icon-frame'>
                <Icon aria-hidden='true' />
              </div>
              <div className='min-w-0'>
                <h2>{t(panel.title)}</h2>
                <p>{t(panel.description)}</p>
              </div>
            </article>
          )
        })}
      </div>
    </section>
  )
}
