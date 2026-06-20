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
  Activity,
  Boxes,
  CircleDollarSign,
  Clapperboard,
  KeyRound,
  RadioTower,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AnimateInView } from '@/components/animate-in-view'

interface CounterProps {
  end: number
  suffix?: string
  duration?: number
}

const GOVERNANCE_PRESSURES = [
  {
    title: 'Provider and protocol drift is constant',
    description:
      'OpenAI, Claude, Gemini, Azure, Bedrock, Vertex, Ollama, and domestic platforms keep changing parameters, models, prices, limits, and multimodal surfaces.',
    icon: RadioTower,
    detail: 'Model access',
  },
  {
    title: 'Agents create long operational chains',
    description:
      'A production Agent may call multiple models, tools, knowledge bases, image or video tasks, and search systems inside one user intent.',
    icon: Boxes,
    detail: 'AgentOps',
  },
  {
    title: 'Cost logic has outgrown token multipliers',
    description:
      'Teams need pricing rules for cache hits, input and output tokens, images, audio, fixed tasks, video duration, stages, quality, and refunds.',
    icon: CircleDollarSign,
    detail: 'Cost audit',
  },
  {
    title: 'Private deployment needs explicit boundaries',
    description:
      'Keys, users, groups, model scope, request logs, admin visibility, retention choices, and security limits need to stay observable and separated.',
    icon: ShieldCheck,
    detail: 'Security boundary',
  },
] as const

const GOVERNANCE_COUNTERS = [
  {
    value: 40,
    suffix: '+',
    label: 'upstream AI ecosystems',
    description:
      'Global and domestic providers, compatible APIs, and local runtimes.',
    icon: RadioTower,
  },
  {
    value: 7,
    suffix: '',
    label: 'governance domains',
    description: 'Access, routing, quota, price, Agent scope, logs, and audit.',
    icon: KeyRound,
  },
  {
    value: 11,
    suffix: '+',
    label: 'async task surfaces',
    description:
      'Submit, poll, map status, proxy results, settle, and refund tasks.',
    icon: Clapperboard,
  },
] as const

const SHIFT_ITEMS = [
  'model invocation -> continuous governance',
  'single key -> users, groups, and Agent tokens',
  'token ratio -> price expressions and rate-cards',
  'proxy logs -> audit boundaries and retention policy',
] as const

function Counter(props: CounterProps) {
  const { end, suffix = '', duration = 1200 } = props
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

  return (
    <section className='home-section home-section--metrics px-4 py-16 sm:px-6 md:py-24'>
      <div className='mx-auto grid max-w-7xl gap-5 lg:grid-cols-[minmax(300px,0.76fr)_minmax(0,1.24fr)]'>
        <AnimateInView className='home-glass-panel home-metrics-intro'>
          <div className='home-section-kicker'>
            <Activity aria-hidden='true' />
            {t('Why governance now')}
          </div>
          <h2>{t('From model invocation to continuous governance')}</h2>
          <p>
            {t(
              'AI products now sit on top of many model platforms, Agent workflows, user systems, billing rules, and security expectations. MAX API turns those moving parts into one operational layer instead of repeated glue code.'
            )}
          </p>
          <div
            className='home-shift-list'
            aria-label={t('Governance shift summary')}
          >
            {SHIFT_ITEMS.map((item) => (
              <span key={item}>{t(item)}</span>
            ))}
          </div>
          <div className='home-governance-counters'>
            {GOVERNANCE_COUNTERS.map((counter) => {
              const Icon = counter.icon
              return (
                <div key={counter.label} className='home-governance-counter'>
                  <Icon aria-hidden='true' />
                  <strong>
                    <Counter end={counter.value} suffix={counter.suffix} />
                  </strong>
                  <span>{t(counter.label)}</span>
                </div>
              )
            })}
          </div>
        </AnimateInView>

        <div className='home-pressure-grid'>
          {GOVERNANCE_PRESSURES.map((pressure, index) => {
            const Icon = pressure.icon
            return (
              <AnimateInView
                key={pressure.title}
                delay={index * 70}
                className='home-glass-panel home-pressure-card'
              >
                <div className='home-pressure-card-header'>
                  <div className='home-icon-frame'>
                    <Icon aria-hidden='true' />
                  </div>
                  <span>{String(index + 1).padStart(2, '0')}</span>
                </div>
                <div className='home-pressure-detail'>{t(pressure.detail)}</div>
                <h3>{t(pressure.title)}</h3>
                <p>{t(pressure.description)}</p>
              </AnimateInView>
            )
          })}
        </div>
      </div>
    </section>
  )
}
