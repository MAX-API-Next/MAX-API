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
import { useCallback, useEffect, useRef } from 'react'
import {
  CircleDollarSign,
  Clapperboard,
  RadioTower,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

interface CounterProps {
  end: number
  suffix?: string
  duration?: number
}

type StatTone = 'cyan' | 'amber' | 'emerald' | 'rose'

type StatItem = {
  icon: LucideIcon
  tone: StatTone
  label: string
  description: string
} & (
  | {
      value: number
      suffix?: string
    }
  | {
      valueText: string
    }
)

const STAT_TONE_CLASSES: Record<
  StatTone,
  {
    icon: string
    badge: string
    border: string
    accent: string
    value: string
  }
> = {
  cyan: {
    icon: 'text-cyan-700 dark:text-cyan-300',
    badge: 'border-cyan-500/25 bg-cyan-500/10',
    border: 'hover:border-cyan-500/45',
    accent: 'bg-cyan-500',
    value: 'text-cyan-900 dark:text-cyan-100',
  },
  amber: {
    icon: 'text-amber-700 dark:text-amber-300',
    badge: 'border-amber-500/25 bg-amber-500/10',
    border: 'hover:border-amber-500/45',
    accent: 'bg-amber-500',
    value: 'text-amber-900 dark:text-amber-100',
  },
  emerald: {
    icon: 'text-emerald-700 dark:text-emerald-300',
    badge: 'border-emerald-500/25 bg-emerald-500/10',
    border: 'hover:border-emerald-500/45',
    accent: 'bg-emerald-500',
    value: 'text-emerald-900 dark:text-emerald-100',
  },
  rose: {
    icon: 'text-rose-700 dark:text-rose-300',
    badge: 'border-rose-500/25 bg-rose-500/10',
    border: 'hover:border-rose-500/45',
    accent: 'bg-rose-500',
    value: 'text-rose-900 dark:text-rose-100',
  },
}

function Counter(props: CounterProps) {
  const { end, suffix = '', duration = 1300 } = props
  const ref = useRef<HTMLSpanElement>(null)
  const startedRef = useRef(false)

  const animate = useCallback(() => {
    const el = ref.current
    if (!el) return
    const start = performance.now()
    const step = (now: number) => {
      const progress = Math.min((now - start) / duration, 1)
      const eased = 1 - Math.pow(1 - progress, 3)
      el.textContent = `${Math.round(eased * end).toLocaleString()}${suffix}`
      if (progress < 1) requestAnimationFrame(step)
    }
    requestAnimationFrame(step)
  }, [duration, end, suffix])

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    if (mq.matches) {
      el.textContent = `${end.toLocaleString()}${suffix}`
      return
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !startedRef.current) {
          startedRef.current = true
          animate()
          observer.unobserve(el)
        }
      },
      { threshold: 0.45 }
    )

    observer.observe(el)
    return () => observer.disconnect()
  }, [animate, end, suffix])

  return (
    <span ref={ref} className='tabular-nums'>
      0{suffix}
    </span>
  )
}

export function Stats() {
  const { t } = useTranslation()
  const stats: StatItem[] = [
    {
      icon: RadioTower,
      tone: 'cyan',
      value: 50,
      suffix: '+',
      label: t('upstream platforms and compatible model ecosystems'),
      description: t(
        'AWS, Azure, Vertex, Ollama, Codex, Dify, RAGFlow, Kling, Seedance, domestic platforms, and OpenAI-compatible APIs.'
      ),
    },
    {
      icon: Clapperboard,
      tone: 'amber',
      value: 11,
      suffix: '+',
      label: t('video task channels and protocol templates'),
      description: t(
        'Task submission, polling, status mapping, error paths, result proxying, and configurable upstream paths.'
      ),
    },
    {
      icon: CircleDollarSign,
      tone: 'emerald',
      valueText: t('Continuously updated'),
      label: t('pricing rule base and rate-card governance'),
      description: t(
        'Model prices, billing expressions, tiered JSON, and task rate-cards are maintained as upstream prices evolve.'
      ),
    },
    {
      icon: ShieldCheck,
      tone: 'rose',
      value: 6,
      suffix: t('agent governance category suffix'),
      label: t('AgentOps governance dimensions'),
      description: t(
        'Token isolation, model scope, quota and cost, routing resilience, usage logs, and audit boundaries.'
      ),
    },
  ]
  const scopeLabels = [
    t('Provider & platform adaptation'),
    t('Protocol layer'),
    t('Cost audit'),
    t('AgentOps'),
  ]

  return (
    <section className='border-border bg-background relative z-10 overflow-hidden border-y px-4 py-14 sm:px-6 md:py-18'>
      <div
        aria-hidden='true'
        className='absolute inset-0 -z-10 [background-image:linear-gradient(to_right,rgba(148,163,184,0.16)_1px,transparent_1px),linear-gradient(to_bottom,rgba(148,163,184,0.12)_1px,transparent_1px)] [background-size:36px_36px] opacity-60 dark:opacity-20'
      />
      <div className='mx-auto grid max-w-7xl gap-5 lg:grid-cols-[minmax(280px,0.74fr)_minmax(0,1.26fr)] lg:items-stretch'>
        <div className='border-border/70 bg-card/72 relative overflow-hidden rounded-xl border p-6 shadow-sm backdrop-blur md:p-8'>
          <span
            aria-hidden='true'
            className='absolute inset-y-0 left-0 w-1 bg-linear-to-b from-cyan-500 via-emerald-500 to-amber-500'
          />
          <div className='flex items-center gap-3'>
            <span className='h-px w-8 bg-emerald-500/70' />
            <p className='text-muted-foreground text-[11px] font-semibold tracking-[0.28em] uppercase'>
              {t('Operational baseline')}
            </p>
          </div>
          <h2 className='mt-4 max-w-xl font-serif text-3xl leading-[1.08] font-black tracking-[-0.01em] md:text-[2.45rem]'>
            {t('Designed for the continuous operation of AI models and Agents')}
          </h2>
          <div className='mt-8 grid gap-2 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2'>
            {scopeLabels.map((label) => (
              <span
                key={label}
                className='border-border/70 bg-background/70 text-muted-foreground rounded-md border px-3 py-2 text-xs font-medium'
              >
                {label}
              </span>
            ))}
          </div>
        </div>

        <div className='grid gap-3 md:grid-cols-2'>
          {stats.map((stat, index) => (
            <StatPanel key={stat.label} stat={stat} index={index} />
          ))}
        </div>
      </div>
    </section>
  )
}

function StatPanel(props: { stat: StatItem; index: number }) {
  const Icon = props.stat.icon
  const tone = STAT_TONE_CLASSES[props.stat.tone]

  return (
    <article
      className={cn(
        'group border-border/70 bg-card/86 hover:bg-card relative flex min-h-[220px] overflow-hidden rounded-xl border p-5 shadow-sm transition-colors md:p-6',
        tone.border
      )}
    >
      <span
        aria-hidden='true'
        className={cn(
          'absolute inset-x-0 top-0 h-0.5 origin-left scale-x-50 transition-transform duration-300 group-hover:scale-x-100',
          tone.accent
        )}
      />
      <div className='flex min-w-0 flex-1 flex-col'>
        <div className='flex items-start justify-between gap-4'>
          <span
            className={cn(
              'inline-flex size-10 shrink-0 items-center justify-center rounded-lg border',
              tone.badge
            )}
          >
            <Icon className={cn('size-4', tone.icon)} aria-hidden='true' />
          </span>
          <span className='editorial-numeral text-muted-foreground/55 text-xs font-bold'>
            {String(props.index + 1).padStart(2, '0')}
          </span>
        </div>

        <div
          className={cn(
            'editorial-numeral mt-5 min-h-12 leading-none font-black tracking-[-0.02em]',
            tone.value,
            'valueText' in props.stat ? 'text-2xl md:text-3xl' : 'text-5xl'
          )}
        >
          {'valueText' in props.stat ? (
            <span className='block leading-[1.08]'>{props.stat.valueText}</span>
          ) : (
            <Counter end={props.stat.value} suffix={props.stat.suffix} />
          )}
        </div>

        <div className='mt-4'>
          <h3 className='text-foreground text-sm leading-5 font-semibold'>
            {props.stat.label}
          </h3>
          <p className='text-muted-foreground mt-2 text-xs leading-5'>
            {props.stat.description}
          </p>
        </div>
      </div>
    </article>
  )
}
