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
import type { CSSProperties } from 'react'
import { Link } from '@tanstack/react-router'
import {
  Activity,
  ArrowRight,
  BookOpen,
  CircleDollarSign,
  Coins,
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

const ORBIT_NODES = [
  { label: 'OpenAI', angle: '18deg', delay: '0s' },
  { label: 'Claude', angle: '78deg', delay: '-1.4s' },
  { label: 'Gemini', angle: '144deg', delay: '-2.5s' },
  { label: 'DeepSeek', angle: '214deg', delay: '-3.8s' },
  { label: 'Qwen', angle: '288deg', delay: '-5.1s' },
  { label: 'Agents', angle: '336deg', delay: '-6.2s' },
] as const

const SPHERE_POINTS = [
  { x: '17%', y: '31%', delay: '0s' },
  { x: '28%', y: '68%', delay: '-0.8s' },
  { x: '44%', y: '23%', delay: '-1.6s' },
  { x: '82%', y: '42%', delay: '-2.1s' },
  { x: '69%', y: '27%', delay: '-2.8s' },
  { x: '78%', y: '63%', delay: '-3.5s' },
  { x: '31%', y: '38%', delay: '-4.2s' },
  { x: '61%', y: '76%', delay: '-4.9s' },
  { x: '23%', y: '49%', delay: '-5.5s' },
  { x: '72%', y: '47%', delay: '-6.1s' },
  { x: '49%', y: '82%', delay: '-6.8s' },
  { x: '58%', y: '18%', delay: '-7.3s' },
] as const

const SPHERE_PANEL_ITEMS = [
  {
    title: 'Ingress',
    value: 'Apps + Agents',
    icon: Network,
  },
  {
    title: 'Policy',
    value: 'users / groups',
    icon: KeyRound,
  },
  {
    title: 'Settlement',
    value: 'rate-card / expr',
    icon: Coins,
  },
  {
    title: 'Audit',
    value: 'logs / limits',
    icon: ShieldCheck,
  },
] as const

const SPHERE_ARCS = [
  { className: 'home-sphere-arc--primary', delay: '0s' },
  { className: 'home-sphere-arc--secondary', delay: '-1.6s' },
  { className: 'home-sphere-arc--tertiary', delay: '-3.1s' },
] as const

const TELEMETRY_ITEMS = [
  'Policy matched',
  'Route selected',
  'Quota reserved',
  'Task observed',
  'Audit sealed',
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
          className='landing-animate-fade-up relative opacity-0'
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

function sphereStyle(values: {
  x: string
  y: string
  delay: string
}): CSSProperties {
  return {
    '--sphere-point-x': values.x,
    '--sphere-point-y': values.y,
    '--sphere-point-delay': values.delay,
  } as CSSProperties
}

function orbitStyle(values: { angle: string; delay: string }): CSSProperties {
  return {
    '--orbit-angle': values.angle,
    '--orbit-delay': values.delay,
  } as CSSProperties
}

function arcStyle(values: { delay: string }): CSSProperties {
  return {
    '--sphere-arc-delay': values.delay,
  } as CSSProperties
}

function NeuralSphere(props: { logo: string; name: string }) {
  const { t } = useTranslation()

  return (
    <div
      className='home-visual-shell'
      aria-label={t('Live AI governance visualization')}
    >
      <div className='home-visual-topbar'>
        <div className='flex items-center gap-2'>
          <span className='home-window-dot' />
          <span className='home-window-dot home-window-dot--secondary' />
          <span className='home-window-dot home-window-dot--tertiary' />
        </div>
        <div className='flex items-center gap-2 text-xs text-slate-400'>
          <Activity aria-hidden='true' className='size-3.5' />
          {t('Governance fabric online')}
        </div>
      </div>

      <div className='home-sphere-stage'>
        <div className='home-sphere-backplane' aria-hidden='true' />
        <div className='home-orbit home-orbit--outer' aria-hidden='true' />
        <div className='home-orbit home-orbit--inner' aria-hidden='true' />
        <div className='home-orbit home-orbit--tilted' aria-hidden='true' />

        {ORBIT_NODES.map((node) => (
          <div
            key={node.label}
            className='home-orbit-node'
            style={orbitStyle(node)}
          >
            <span>{node.label}</span>
          </div>
        ))}

        <div className='home-sphere' aria-hidden='true'>
          <div className='home-sphere-grid home-sphere-grid--lat' />
          <div className='home-sphere-grid home-sphere-grid--lng' />
          <div className='home-sphere-halo home-sphere-halo--equator' />
          <div className='home-sphere-halo home-sphere-halo--meridian' />
          {SPHERE_ARCS.map((arc) => (
            <span
              key={arc.className}
              className={cn('home-sphere-arc', arc.className)}
              style={arcStyle(arc)}
            />
          ))}
          <div className='home-sphere-core'>
            <img
              src={props.logo}
              alt={props.name}
              className='home-sphere-logo'
            />
          </div>
          {SPHERE_POINTS.map((point, index) => (
            <span
              key={`${point.x}-${point.y}`}
              className='home-sphere-point'
              style={sphereStyle(point)}
              data-index={index}
            />
          ))}
        </div>
      </div>

      <div className='home-visual-panels'>
        {SPHERE_PANEL_ITEMS.map((item) => {
          const Icon = item.icon
          return (
            <div key={item.title} className='home-visual-panel'>
              <Icon aria-hidden='true' />
              <div>
                <span>{t(item.title)}</span>
                <strong>{t(item.value)}</strong>
              </div>
            </div>
          )
        })}
      </div>

      <div className='home-telemetry-band' aria-hidden='true'>
        <div className='home-telemetry-track'>
          {[...TELEMETRY_ITEMS, ...TELEMETRY_ITEMS].map((item, index) => (
            <span key={`${item}-${index}`}>{t(item)}</span>
          ))}
        </div>
      </div>
    </div>
  )
}
